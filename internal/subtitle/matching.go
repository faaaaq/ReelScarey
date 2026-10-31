package subtitle

import (
	"slices"
	"strings"
)

func rank(p Preferences, t Target, in []Candidate) []Candidate {
	out := make([]Candidate, 0, len(in))
	seen := map[int64]bool{}
	for _, c := range in {
		if seen[c.FileID] || c.FileID <= 0 {
			continue
		}
		seen[c.FileID] = true
		c.Reasons = []string{}
		c.Format = candidateFormat(p, c)
		reject := func(message string) { c.Reasons = append(c.Reasons, message) }
		if c.Format == "" {
			reject("The original file is not a supported SRT, VTT, ASS or SSA subtitle. Choose SRT output to request conversion.")
		}
		if c.Provider == "subdl" && subtitleExtension(c.FileName) != "."+c.Format {
			reject("SubDL provides original files only. Choose original format or an SRT candidate.")
		}
		if !c.Identity {
			reject("The provider did not confirm this movie or episode identity.")
		}
		if !slices.Contains(p.Tracks, c.Track) {
			reject("This language or track type is not requested.")
		}
		if c.Translated {
			reject("AI and machine translated subtitles are excluded.")
		}
		if c.Multipart {
			reject("Subtitles split across multiple discs are not supported.")
		}
		if c.HearingImpaired && p.HearingImpaired == "exclude" {
			reject("Hearing-impaired tracks are excluded by your preferences.")
		}
		c.CanDownload = len(c.Reasons) == 0
		releaseMatch := releaseName(c.Release) != "" && releaseName(c.Release) == releaseName(t.Release) && len(t.Episodes) <= 1
		c.Automatic = c.CanDownload && !c.RequireReview && (c.HashMatch || (p.MatchPolicy == "hash_or_release" && releaseMatch))
		if c.CanDownload && !c.Automatic {
			reject("Timing is uncertain: there is no acceptable file-hash or exact release match. Review before downloading.")
		}
		c.Score = 0
		if c.HashMatch {
			c.Score += 10000
		}
		if releaseMatch {
			c.Score += 5000
		}
		if c.Trusted {
			c.Score += 100
		}
		if p.HearingImpaired == "prefer" && c.HearingImpaired {
			c.Score += 20
		} else if p.HearingImpaired != "prefer" && !c.HearingImpaired {
			c.Score += 10
		}
		c.Score += min(c.Downloads, 9)
		out = append(out, c)
	}
	slices.SortStableFunc(out, func(a, b Candidate) int {
		if a.CanDownload != b.CanDownload {
			if a.CanDownload {
				return -1
			}
			return 1
		}
		if a.Automatic != b.Automatic {
			if a.Automatic {
				return -1
			}
			return 1
		}
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if n := strings.Compare(a.Release, b.Release); n != 0 {
			return n
		}
		if a.FileID < b.FileID {
			return -1
		}
		if a.FileID > b.FileID {
			return 1
		}
		return 0
	})
	return out
}
