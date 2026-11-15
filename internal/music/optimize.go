package music

import (
	"context"
	"math"
	"strings"
)

// maxOptimizeFiles bounds one optimization call. The preview probes every
// requested file with ffprobe, so an unbounded selection could stall the
// request for minutes; callers page through the library instead.
const maxOptimizeFiles = 1000

// optimizeContainerOverhead is applied to the raw audio estimate for lossy
// targets. Container headers, metadata and padding add roughly 1-3% to the
// pure audio bitrate, so the preview claims slightly more space than the
// bitrate math alone would predict.
const optimizeContainerOverhead = 1.02

// optimizeChunkSize bounds the number of parameters in one file-ID lookup so
// the query stays far below any SQLite variable limit.
const optimizeChunkSize = 250

// OptimizationItem is the dry-run verdict for one file under a profile.
type OptimizationItem struct {
	FileID            int64  `json:"file_id"`
	RelativePath      string `json:"relative_path"`
	Format            string `json:"format"`
	Bitrate           int    `json:"bitrate"`
	SizeBytes         int64  `json:"size_bytes"`
	Lossless          bool   `json:"lossless"`
	Eligible          bool   `json:"eligible"`
	NotEligibleReason string `json:"not_eligible_reason,omitempty"`
	EstimatedBytes    int64  `json:"estimated_bytes"`
}

// OptimizationPreview is the result of a dry run. Totals and savings are
// computed over the whole inspected selection; ineligible items keep their
// current size as the estimate, so EstimatedSavings only reflects files that
// would actually be re-encoded.
type OptimizationPreview struct {
	Profile             string             `json:"profile"`
	Items               []OptimizationItem `json:"items"`
	TotalCurrentBytes   int64              `json:"total_current_bytes"`
	TotalEstimatedBytes int64              `json:"total_estimated_bytes"`
	EstimatedSavings    int64              `json:"estimated_savings"`
	EligibleCount       int                `json:"eligible_count"`
	TotalCount          int                `json:"total_count"`
}

// PreviewOptimization is a dry run: it probes the selected files and reports
// per-file eligibility and size estimates for a profile. It never writes
// anything — the prober is read-only and every other input is a database
// read. Empty fileIDs mean all registered files, still bounded by
// maxOptimizeFiles. An unknown profile key is ErrInvalidInput.
func (s *Service) PreviewOptimization(ctx context.Context, profileKey string, fileIDs []int64) (OptimizationPreview, error) {
	preview, _, err := s.planOptimization(ctx, profileKey, fileIDs)
	return preview, err
}

// ApplyOptimization enqueues a library conversion job for every eligible file
// and returns the number of enqueued jobs. It never deletes, converts or
// publishes anything itself: the conversion queue owns conversion, publication
// and original retention. The queue deduplicates, so an already-queued file is
// not converted twice. If an enqueue fails, the error is returned together
// with the number of jobs enqueued before it.
func (s *Service) ApplyOptimization(ctx context.Context, profileKey string, fileIDs []int64) (int64, error) {
	_, eligible, err := s.planOptimization(ctx, profileKey, fileIDs)
	if err != nil {
		return 0, err
	}
	var enqueued int64
	for _, file := range eligible {
		if _, err := s.EnqueueConversion(ctx, "library", file.ID, profileKey, 8); err != nil {
			return enqueued, err
		}
		enqueued++
	}
	return enqueued, nil
}

// planOptimization resolves the profile and files, then builds the dry-run
// preview. The returned slice lists the files that ApplyOptimization would
// enqueue, in the same order.
func (s *Service) planOptimization(ctx context.Context, profileKey string, fileIDs []int64) (OptimizationPreview, []MusicFile, error) {
	profile, err := s.ResolveProfileKey(ctx, profileKey)
	if err != nil {
		return OptimizationPreview{}, nil, err
	}
	files, err := s.resolveOptimizationFiles(ctx, fileIDs)
	if err != nil {
		return OptimizationPreview{}, nil, err
	}
	preview := OptimizationPreview{Profile: profileKey, Items: []OptimizationItem{}}
	eligible := []MusicFile{}
	for _, file := range files {
		item := s.optimizeItem(ctx, file, profile)
		preview.Items = append(preview.Items, item)
		preview.TotalCount++
		preview.TotalCurrentBytes += item.SizeBytes
		preview.TotalEstimatedBytes += item.EstimatedBytes
		if item.Eligible {
			preview.EligibleCount++
			eligible = append(eligible, file)
		}
	}
	preview.EstimatedSavings = preview.TotalCurrentBytes - preview.TotalEstimatedBytes
	return preview, eligible, nil
}

// resolveOptimizationFiles loads the file rows for a call. Explicit IDs are
// deduplicated and returned in file-ID order; an empty selection pages through
// the whole library, bounded by maxOptimizeFiles.
func (s *Service) resolveOptimizationFiles(ctx context.Context, fileIDs []int64) ([]MusicFile, error) {
	if len(fileIDs) > maxOptimizeFiles {
		return nil, ErrInvalidInput
	}
	if len(fileIDs) == 0 {
		out := []MusicFile{}
		for page := 1; page <= 100000; page++ {
			batch, err := s.store.ListFiles(ctx, page)
			if err != nil {
				return nil, err
			}
			out = append(out, batch.Items...)
			if len(batch.Items) == 0 || len(out) >= maxOptimizeFiles || int64(page)*int64(len(batch.Items)) >= int64(batch.Total) {
				break
			}
		}
		if len(out) > maxOptimizeFiles {
			out = out[:maxOptimizeFiles]
		}
		return out, nil
	}
	out := []MusicFile{}
	seen := map[int64]bool{}
	for start := 0; start < len(fileIDs); start += optimizeChunkSize {
		end := start + optimizeChunkSize
		if end > len(fileIDs) {
			end = len(fileIDs)
		}
		chunk := fileIDs[start:end]
		placeholders := make([]string, len(chunk))
		args := make([]any, 0, len(chunk))
		for i, id := range chunk {
			placeholders[i] = "?"
			args = append(args, id)
		}
		rows, err := s.store.db.QueryContext(ctx, `SELECT f.id,f.relative_path,f.format,f.duration_ms,f.size_bytes,f.variant_role
			FROM music_files f WHERE f.id IN (`+strings.Join(placeholders, ",")+`) ORDER BY f.id`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var file MusicFile
			if err := rows.Scan(&file.ID, &file.RelativePath, &file.Format, &file.DurationMS, &file.SizeBytes, &file.VariantRole); err != nil {
				rows.Close()
				return nil, err
			}
			if !seen[file.ID] {
				seen[file.ID] = true
				out = append(out, file)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// optimizeItem decides one file's eligibility under the profile. The order of
// the checks matters: the profile decision comes first, then structural
// reasons, then the honest probe result, then the conversion decision.
func (s *Service) optimizeItem(ctx context.Context, file MusicFile, profile QualityProfile) OptimizationItem {
	item := OptimizationItem{
		FileID:         file.ID,
		RelativePath:   file.RelativePath,
		Format:         file.Format,
		SizeBytes:      file.SizeBytes,
		EstimatedBytes: file.SizeBytes, // unchanged unless the file is eligible
	}
	if profile.Codec == "original" {
		item.NotEligibleReason = "profile does not convert"
		return item
	}
	if file.VariantRole != "" && file.VariantRole != "primary" {
		item.NotEligibleReason = "the file is a variant copy produced by an earlier conversion and is never re-converted"
		return item
	}
	if s.prober == nil {
		item.NotEligibleReason = "source could not be inspected"
		return item
	}
	path, _, err := s.safeMediaPath(file.RelativePath)
	if err != nil {
		item.NotEligibleReason = "source could not be inspected"
		return item
	}
	tags, err := s.prober.ProbeAudio(ctx, path)
	if err != nil {
		item.NotEligibleReason = "source could not be inspected"
		return item
	}
	item.Bitrate = tags.Bitrate
	item.Lossless = sourceIsLossless(probeFormatFamily(tags.Format))
	if profile.RequireLossless && !item.Lossless {
		item.NotEligibleReason = "requires lossless source"
		return item
	}
	if needed, reason := ConversionNeeded(tags, profile); !needed {
		item.NotEligibleReason = reason
		return item
	}
	item.Eligible = true
	item.EstimatedBytes = estimateOptimizedBytes(file, tags, profile)
	return item
}

// estimateOptimizedBytes estimates the converted file's size. For lossy
// targets the estimate is bitrate_kbps * duration_ms / 8 bytes: the target
// kilobits per second times the duration in milliseconds gives total kilobits
// (kbps * ms = kbit * 1000 * s / 1000), and eight bits per byte divides by 8.
// A small container overhead factor is added on top. For lossless targets the
// source size is reused — re-encoding a lossy source to FLAC cannot shrink it
// and FLAC compression of real audio is not claimed as savings. When the
// duration is unknown the source size is kept so the preview never overstates
// savings.
func estimateOptimizedBytes(file MusicFile, tags AudioTags, profile QualityProfile) int64 {
	if profile.BitrateKbps <= 0 {
		return file.SizeBytes
	}
	duration := tags.DurationMS
	if duration <= 0 {
		duration = file.DurationMS
	}
	if duration <= 0 {
		return file.SizeBytes
	}
	return int64(math.Ceil(float64(profile.BitrateKbps) * float64(duration) / 8 * optimizeContainerOverhead))
}
