package music

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

const (
	acoustidEndpoint = "https://api.acoustid.org/v2/lookup"
	// acoustidMinInterval is a conservative client-side spacing. AcoustID's
	// published limit is three requests per second.
	acoustidMinInterval = 350 * time.Millisecond
	acoustidMaxBody     = 2 << 20
	acoustidMaxResults  = 100
	acoustidMinScore    = 0.85
	acoustidKeyName     = "acoustid"
)

// AcoustIDStatus is the administrator-facing key and tool status. The saved key
// is never included.
type AcoustIDStatus struct {
	Configured           bool   `json:"configured"`
	UpdatedAt            string `json:"updated_at"`
	FingerprintAvailable bool   `json:"fingerprint_available"`
	Message              string `json:"message"`
}

// ValidAcoustIDKey accepts an application key of 8–128 printable, non-control
// characters. The provider issues application keys, not user credentials.
func ValidAcoustIDKey(value string) bool {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		return false
	}
	count := utf8.RuneCountInString(value)
	if count < 8 || count > 128 || strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	for _, r := range value {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func (s *Service) SetSecrets(store *securestore.Store) {
	s.secrets = store
}

// setAcoustIDClient replaces the fixed provider for tests. Production always
// uses NewAcoustID, whose endpoint is a compile-time constant.
func (s *Service) setAcoustIDClient(client *AcoustIDClient) {
	if client != nil {
		s.acoustid = client
	}
}

// AcoustIDStatus reads only the key presence, its update time and fpcalc
// availability. It never reveals the key and never reads a media file.
func (s *Service) AcoustIDStatus(ctx context.Context) (AcoustIDStatus, error) {
	status := AcoustIDStatus{}
	if s.secrets != nil {
		raw, err := s.secrets.GetNamed(ctx, acoustidKeyName)
		if err != nil {
			return status, err
		}
		status.Configured = len(strings.TrimSpace(string(raw))) > 0
		if status.Configured {
			updated, err := s.integrationUpdatedAt(ctx, acoustidKeyName)
			if err != nil {
				return status, err
			}
			status.UpdatedAt = updated
		}
	}
	status.FingerprintAvailable = s.fingerprinter != nil && s.fingerprinter.Available(ctx) == nil
	status.Message = acoustidStatusMessage(status.Configured, status.FingerprintAvailable)
	return status, nil
}

func acoustidStatusMessage(configured, fingerprintAvailable bool) string {
	switch {
	case !fingerprintAvailable && !configured:
		return "Chromaprint fpcalc is not available and no AcoustID key is saved. Use the Docker image, which includes libchromaprint-tools, and add an application key from https://acoustid.org/new-application."
	case !fingerprintAvailable:
		return "Chromaprint fpcalc is not available in this deployment. Use the ReelScarey Docker image, which includes libchromaprint-tools, or set REELSCAREY_FPCALC_PATH."
	case !configured:
		return "Add an AcoustID application key from https://acoustid.org/new-application to fingerprint unresolved files."
	default:
		return "AcoustID matching is ready. Only the Chromaprint fingerprint and whole-file duration are sent to AcoustID; audio is never uploaded."
	}
}

func (s *Service) SaveAcoustIDKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if !ValidAcoustIDKey(key) {
		return ErrInvalidInput
	}
	if s.secrets == nil {
		return &Error{Code: "music_acoustid_not_configured", Message: "AcoustID key storage is unavailable. Configure the application secret store and try again."}
	}
	return s.secrets.SetNamed(ctx, acoustidKeyName, []byte(key))
}

func (s *Service) DeleteAcoustIDKey(ctx context.Context) error {
	if s.secrets == nil {
		return &Error{Code: "music_acoustid_not_configured", Message: "AcoustID key storage is unavailable. Configure the application secret store and try again."}
	}
	return s.secrets.DeleteNamed(ctx, acoustidKeyName)
}

func (s *Service) integrationUpdatedAt(ctx context.Context, name string) (string, error) {
	var updated string
	err := s.store.db.QueryRowContext(ctx, `SELECT updated_at FROM integration_configs WHERE name=?`, name).Scan(&updated)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return updated, err
}

func (s *Service) acoustidKey(ctx context.Context) (string, error) {
	if s.secrets == nil {
		return "", &Error{Code: "music_acoustid_not_configured", Message: "AcoustID is not configured. Save an application key in Music import settings."}
	}
	raw, err := s.secrets.GetNamed(ctx, acoustidKeyName)
	if err != nil {
		return "", &Error{Code: "music_acoustid_not_configured", Message: "The saved AcoustID key could not be read. Restore the matching secrets.key and save the key again."}
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", &Error{Code: "music_acoustid_not_configured", Message: "AcoustID is not configured. Add an application key from https://acoustid.org/new-application."}
	}
	if !ValidAcoustIDKey(key) {
		return "", &Error{Code: "music_acoustid_invalid_key", Message: "The saved AcoustID key is invalid. Save a new application key."}
	}
	return key, nil
}

// AcoustIDReleaseGroup is a bounded release-group projection from the provider.
type AcoustIDReleaseGroup struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Type    string   `json:"type"`
	Artists []string `json:"artists"`
}

// AcoustIDCandidate is one AcoustID result, retaining the recording and release
// group identities needed for the strict matching policy.
type AcoustIDCandidate struct {
	Score          float64                `json:"score"`
	RecordingID    string                 `json:"recording_id"`
	RecordingTitle string                 `json:"recording_title"`
	ReleaseGroups  []AcoustIDReleaseGroup `json:"release_groups"`
}

// AcoustIDMatch is the single release group and recording that survived the
// confidence policy. RecordingID is empty when two equally scored recordings
// share the release group, so the caller falls back to existing tag evidence.
type AcoustIDMatch struct {
	ReleaseGroupID    string
	ReleaseGroupTitle string
	RecordingID       string
	RecordingTitle    string
	Score             float64
}

// AcoustIDClient talks to the fixed AcoustID lookup endpoint. It never follows
// redirects and spaces sequential requests to respect the published limit.
type AcoustIDClient struct {
	endpoint string
	http     *http.Client
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
}

func NewAcoustID() *AcoustIDClient {
	return newAcoustID(acoustidEndpoint, &http.Client{Timeout: 15 * time.Second}, acoustidMinInterval)
}

func newAcoustID(endpoint string, client *http.Client, interval time.Duration) *AcoustIDClient {
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("provider redirects are disabled") }
	return &AcoustIDClient{endpoint: strings.TrimRight(endpoint, "/"), http: &copy, interval: interval}
}

func (c *AcoustIDClient) wait(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	delay := time.Until(c.next)
	if delay < 0 {
		delay = 0
	}
	c.next = now.Add(delay + c.interval)
	c.mu.Unlock()
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return &Error{Code: "music_acoustid_unavailable", Message: "The AcoustID request was cancelled."}
	case <-timer.C:
		return nil
	}
}

type acoustidResponse struct {
	Status  string             `json:"status"`
	Results []acoustidResult   `json:"results"`
	Error   acoustidErrorField `json:"error"`
}

type acoustidErrorField struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type acoustidResult struct {
	ID         string              `json:"id"`
	Score      float64             `json:"score"`
	Recordings []acoustidRecording `json:"recordings"`
}

type acoustidRecording struct {
	ID            string                 `json:"id"`
	Title         string                 `json:"title"`
	Artists       []acoustidArtist       `json:"artists"`
	ReleaseGroups []acoustidReleaseGroup `json:"releasegroups"`
}

type acoustidArtist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type acoustidReleaseGroup struct {
	ID      string           `json:"id"`
	Title   string           `json:"title"`
	Type    string           `json:"type"`
	Artists []acoustidArtist `json:"artists"`
}

// Lookup performs one bounded GET against the fixed AcoustID endpoint. An empty
// result set is a provider no-match, not a transport error.
func (c *AcoustIDClient) Lookup(ctx context.Context, key string, duration int, fingerprint string) ([]AcoustIDCandidate, error) {
	if !ValidAcoustIDKey(key) || !validDurationSeconds(duration) || !validFingerprintValue(fingerprint) {
		return nil, &Error{Code: "music_acoustid_invalid_request", Message: "AcoustID needs a valid application key, duration and Chromaprint fingerprint."}
	}
	if err := c.wait(ctx); err != nil {
		return nil, err
	}
	values := url.Values{
		"client":      {key},
		"duration":    {strconv.Itoa(duration)},
		"fingerprint": {fingerprint},
		"meta":        {"recordings+releasegroups"},
		"format":      {"json"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return nil, &Error{Code: "music_acoustid_unavailable", Message: "AcoustID could not be reached."}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &Error{Code: "music_acoustid_unavailable", Message: "AcoustID could not be reached. Check your network and try again."}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return nil, &Error{Code: "music_acoustid_invalid_key", Message: "AcoustID rejected the application key. Create an application key at https://acoustid.org/new-application and save it again."}
	case http.StatusNotFound:
		return nil, &Error{Code: "music_acoustid_no_match", Message: "AcoustID found no match for this fingerprint."}
	case http.StatusTooManyRequests:
		return nil, acoustidRateLimitError(resp.Header.Get("Retry-After"))
	default:
		return nil, &Error{Code: "music_acoustid_unavailable", Message: "AcoustID is temporarily unavailable. Try again later."}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, acoustidMaxBody+1))
	if err != nil || int64(len(body)) > acoustidMaxBody {
		return nil, &Error{Code: "music_acoustid_invalid_response", Message: "AcoustID returned an unreadable response. Try again later."}
	}
	var payload acoustidResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &Error{Code: "music_acoustid_invalid_response", Message: "AcoustID returned an unreadable response. Try again later."}
	}
	if !strings.EqualFold(payload.Status, "ok") {
		return nil, acoustidProviderError(payload.Error)
	}
	if len(payload.Results) == 0 {
		return nil, &Error{Code: "music_acoustid_no_match", Message: "AcoustID found no match for this fingerprint."}
	}
	if len(payload.Results) > acoustidMaxResults {
		return nil, invalidAcoustIDResponse()
	}
	candidates := make([]AcoustIDCandidate, 0, len(payload.Results))
	for _, result := range payload.Results {
		if math.IsNaN(result.Score) || math.IsInf(result.Score, 0) || result.Score < 0 || result.Score > 1 {
			return nil, invalidAcoustIDResponse()
		}
		if len(result.Recordings) > acoustidMaxResults {
			return nil, invalidAcoustIDResponse()
		}
		for _, recording := range result.Recordings {
			if len(recording.ReleaseGroups) > acoustidMaxResults {
				return nil, invalidAcoustIDResponse()
			}
			candidate := AcoustIDCandidate{Score: result.Score}
			if ValidMBID(recording.ID) {
				candidate.RecordingID = recording.ID
				candidate.RecordingTitle, _ = cleanText(recording.Title, 1000)
			}
			for _, group := range recording.ReleaseGroups {
				if !ValidMBID(group.ID) {
					continue
				}
				groupTitle, _ := cleanText(group.Title, 1000)
				names := make([]string, 0, len(group.Artists))
				for _, artist := range group.Artists {
					if name, ok := cleanText(artist.Name, 500); ok {
						names = append(names, name)
					}
				}
				candidate.ReleaseGroups = append(candidate.ReleaseGroups, AcoustIDReleaseGroup{
					ID: group.ID, Title: groupTitle, Type: strings.TrimSpace(group.Type), Artists: names,
				})
			}
			candidates = append(candidates, candidate)
		}
	}
	return candidates, nil
}

func acoustidRateLimitError(value string) *Error {
	seconds := 5
	if value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			seconds = n
		}
	}
	seconds = min(300, max(1, seconds))
	return &Error{Code: "music_acoustid_rate_limited", Message: "AcoustID is limiting requests. Wait before trying again.", RetryAfter: seconds}
}

func acoustidProviderError(field acoustidErrorField) *Error {
	switch field.Code {
	case 2, 3:
		return &Error{Code: "music_acoustid_invalid_key", Message: "AcoustID rejected the application key. Create an application key at https://acoustid.org/new-application and save it again."}
	case 1, 4, 5:
		return &Error{Code: "music_acoustid_invalid_request", Message: "AcoustID rejected the fingerprint request. Fingerprint the file again."}
	default:
		return invalidAcoustIDResponse()
	}
}

func invalidAcoustIDResponse() *Error {
	return &Error{Code: "music_acoustid_invalid_response", Message: "AcoustID returned an invalid response. Try again later."}
}

// ResolveAcoustID applies the strict owner policy: only scores of at least 0.85
// count, and the accepted candidates must name exactly one release group. It
// never guesses between low scores or multiple albums.
func ResolveAcoustID(candidates []AcoustIDCandidate) (AcoustIDMatch, error) {
	accepted := make([]AcoustIDCandidate, 0, len(candidates))
	groups := map[string]AcoustIDReleaseGroup{}
	for _, candidate := range candidates {
		if candidate.Score < acoustidMinScore {
			continue
		}
		accepted = append(accepted, candidate)
		for _, group := range candidate.ReleaseGroups {
			if ValidMBID(group.ID) {
				groups[strings.ToLower(group.ID)] = group
			}
		}
	}
	if len(accepted) == 0 {
		return AcoustIDMatch{}, &Error{Code: "music_acoustid_no_confident_match", Message: "AcoustID returned no match with a score of at least 0.85. Add MusicBrainz tags or the release-group ID and try again."}
	}
	switch len(groups) {
	case 0:
		return AcoustIDMatch{}, &Error{Code: "music_acoustid_no_release_group", Message: "AcoustID matched the fingerprint but returned no release group. Add album tags or the MusicBrainz release-group ID and try again."}
	case 1:
	default:
		return AcoustIDMatch{}, &Error{Code: "music_acoustid_ambiguous_release_group", Message: "AcoustID matched more than one release group. Add the MusicBrainz release-group ID or correct the album tags; the file stays unresolved instead of guessing."}
	}
	groupID := ""
	for id := range groups {
		groupID = id
	}
	group := groups[groupID]
	match := AcoustIDMatch{ReleaseGroupID: groupID, ReleaseGroupTitle: group.Title}
	tied := false
	for _, candidate := range accepted {
		namesGroup := false
		for _, candidateGroup := range candidate.ReleaseGroups {
			if strings.EqualFold(candidateGroup.ID, groupID) {
				namesGroup = true
				break
			}
		}
		if !namesGroup {
			continue
		}
		switch {
		case candidate.Score > match.Score:
			match.Score = candidate.Score
			match.RecordingID = candidate.RecordingID
			match.RecordingTitle = candidate.RecordingTitle
			tied = false
		case candidate.Score == match.Score && candidate.RecordingID != match.RecordingID:
			tied = true
		}
	}
	if tied {
		// Two equally scored recordings share the release group. Keep the
		// album identity but let existing tag/track evidence choose the track.
		match.RecordingID, match.RecordingTitle = "", ""
	}
	return match, nil
}
