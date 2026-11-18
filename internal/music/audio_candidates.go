package music

import (
	"fmt"
	"sort"
	"strings"
)

var audioVersionWords = []struct {
	name     string
	patterns []string
}{
	{"radio edit", []string{"radio edit", "radio-edit"}},
	{"sped up", []string{"sped up", "speed up", "sped-up", "nightcore"}},
	{"instrumental", []string{"instrumental"}},
	{"acoustic", []string{"acoustic"}},
	{"remastered", []string{"remaster", "remastered"}},
	{"deluxe", []string{"deluxe"}},
	{"extended", []string{"extended"}},
	{"remix", []string{"remix"}},
	{"cover", []string{"cover"}},
	{"live", []string{"live"}},
	{"demo", []string{"demo"}},
}

// audioVersionQualifiers separates meaningful recording/version evidence from
// codec and "official video" decoration. An empty set means the ordinary
// studio/unspecified version; live, cover, remix and other versions are not
// silently folded into it.
func audioVersionQualifiers(value string) []string {
	lower := strings.ToLower(strings.NewReplacer("_", " ", "-", " ").Replace(value))
	lower = strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ", ",", " ").Replace(lower)
	lower = strings.Join(strings.Fields(lower), " ")
	out := []string{}
	for _, qualifier := range audioVersionWords {
		for _, pattern := range qualifier.patterns {
			pattern = strings.ReplaceAll(pattern, "-", " ")
			if strings.Contains(" "+lower+" ", " "+pattern+" ") {
				out = append(out, qualifier.name)
				break
			}
		}
	}
	return out
}

func sameAudioVersion(a, b []string) bool {
	return strings.Join(a, ",") == strings.Join(b, ",")
}

func audioChannelMatchesArtist(channel, artist string) bool {
	channel = strings.TrimSpace(channel)
	artist = strings.TrimSpace(artist)
	if channel == "" || artist == "" {
		return false
	}
	for _, suffix := range []string{" - Topic", " VEVO", " Official"} {
		if strings.HasSuffix(strings.ToLower(channel), strings.ToLower(suffix)) {
			channel = strings.TrimSpace(channel[:len(channel)-len(suffix)])
		}
	}
	channelKey, artistKey := normalizeMatchName(channel), normalizeMatchName(artist)
	return channelKey != "" && artistKey != "" && (channelKey == artistKey || len(artistKey) >= 4 && strings.Contains(channelKey, artistKey))
}

// evaluateAudioCandidates ranks all evidence but auto-selects only a unique
// eligible winner. Missing duration contributes no points and never makes the
// first title match authoritative. Contradictory artist/version evidence makes
// a row ineligible, while an equal-score top set stays unchosen for review.
func evaluateAudioCandidates(candidates []AudioCandidate, request AcquisitionTrackInput) AudioCandidateDecision {
	decision := AudioCandidateDecision{Candidates: []AudioCandidateEvidence{}}
	requestedVersion := audioVersionQualifiers(request.Version)
	if strings.TrimSpace(request.Version) == "" {
		requestedVersion = audioVersionQualifiers(request.Title)
	}
	expected := int64(request.DurationMS / 1000)
	for _, candidate := range candidates {
		evidence := AudioCandidateEvidence{Candidate: candidate, Score: 100, Reasons: []string{}, Eligible: true}
		if !audioCandidateMatchesTitle(candidate.Title, request.Title) {
			evidence.Eligible = false
			evidence.Score = 0
			evidence.Reasons = append(evidence.Reasons, "Track title does not match the requested recording.")
		} else {
			evidence.Reasons = append(evidence.Reasons, "Track title matches on token boundaries.")
		}
		candidateVersion := audioVersionQualifiers(candidate.Title)
		if !sameAudioVersion(requestedVersion, candidateVersion) {
			evidence.Eligible = false
			evidence.Reasons = append(evidence.Reasons, fmt.Sprintf("Version differs: requested %v, candidate %v.", requestedVersion, candidateVersion))
		} else {
			evidence.Score += 40
			evidence.Reasons = append(evidence.Reasons, "Version qualifiers agree.")
		}
		if strings.TrimSpace(request.Artist) != "" && strings.TrimSpace(candidate.Channel) != "" {
			if audioChannelMatchesArtist(candidate.Channel, request.Artist) {
				evidence.Score += 20
				evidence.Reasons = append(evidence.Reasons, "Channel agrees with the requested artist.")
			} else {
				evidence.Eligible = false
				evidence.Reasons = append(evidence.Reasons, "Channel conflicts with the requested artist; review instead of assuming identity.")
			}
		} else {
			evidence.Reasons = append(evidence.Reasons, "Artist/channel evidence is unavailable; other evidence remains usable.")
		}
		if expected > 0 && candidate.DurationSeconds > 0 {
			diff := candidate.DurationSeconds - expected
			if diff < 0 {
				diff = -diff
			}
			evidence.DurationDifference = &diff
			if diff > 10 {
				evidence.Eligible = false
				evidence.Reasons = append(evidence.Reasons, fmt.Sprintf("Duration differs by %d seconds, beyond the 10-second tolerance.", diff))
			} else {
				evidence.Score += 20 - int(diff)
				evidence.Reasons = append(evidence.Reasons, fmt.Sprintf("Duration is %d seconds from the requested recording.", diff))
			}
		} else {
			evidence.Reasons = append(evidence.Reasons, "Duration evidence is unavailable and is neutral, not an exact match.")
		}
		decision.Candidates = append(decision.Candidates, evidence)
	}
	sort.SliceStable(decision.Candidates, func(i, j int) bool {
		if decision.Candidates[i].Score != decision.Candidates[j].Score {
			return decision.Candidates[i].Score > decision.Candidates[j].Score
		}
		return decision.Candidates[i].Candidate.ID < decision.Candidates[j].Candidate.ID
	})
	eligible := []AudioCandidateEvidence{}
	for _, candidate := range decision.Candidates {
		if candidate.Eligible {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		decision.Reason = "No candidate has compatible title, artist, version and duration evidence. Review the returned candidates."
		return decision
	}
	if len(eligible) > 1 && eligible[0].Score == eligible[1].Score {
		decision.Reason = "Multiple candidates have equally strong evidence. Choose a source explicitly; ReelScarey will not use search order to decide."
		return decision
	}
	chosen := eligible[0].Candidate
	decision.Chosen = &chosen
	decision.Reason = "One candidate has the strongest compatible evidence."
	return decision
}

// chooseAudioCandidate retains the legacy narrow call for tests and other
// packages; acquisition uses the full evidence decision directly.
func chooseAudioCandidate(candidates []AudioCandidate, durationMS int, title string) *AudioCandidate {
	return evaluateAudioCandidates(candidates, AcquisitionTrackInput{Title: title, DurationMS: durationMS}).Chosen
}
