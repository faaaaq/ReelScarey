package releaseguard

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// MaxWebhookBody bounds completion payloads before they are decoded. Both
// providers send small notification documents; anything larger is rejected
// instead of being buffered or echoed.
const MaxWebhookBody = 64 << 10

const (
	// WebhookProviderSABnzbd identifies payloads parsed by ParseSABnzbd.
	WebhookProviderSABnzbd = "sabnzbd"
	// WebhookProviderNZBGet identifies payloads parsed by ParseNZBGet.
	WebhookProviderNZBGet = "nzbget"
)

// WebhookResult identifies a completed or failed Usenet job. State is either
// StateCompleted or StateFailed. FailureReason is bounded display text and must
// not be treated as a trusted error code. Provider credentials and unknown
// fields are never copied into the result.
type WebhookResult struct {
	Provider      string `json:"provider"`
	ReleaseName   string `json:"release_name"`
	JobID         string `json:"job_id"`
	Category      string `json:"category"`
	State         string `json:"state"`
	FailureReason string `json:"failure_reason,omitempty"`
}

// IdentityKey returns a stable Usenet release key for the parsed job. It prefers
// the provider GUID and falls back to a normalized title only when no GUID was
// supplied.
func (r WebhookResult) IdentityKey() (string, error) {
	if id := strings.TrimSpace(r.JobID); id != "" {
		return UsenetKey(id)
	}
	if name := strings.TrimSpace(r.ReleaseName); name != "" {
		return UsenetTitleKey(name)
	}
	return "", invalid("completion webhook did not identify a release")
}

// ParseSABnzbd parses a bounded SABnzbd completion/failure notification. It
// accepts the JSON notification shape (event/type plus nzo_id, name, category,
// status/fail_msg) and the equivalent form-encoded POST. SABnzbd sends both
// shapes in deployed notification hooks.
func ParseSABnzbd(body []byte) (WebhookResult, error) {
	fields, err := parseWebhookFields(body)
	if err != nil {
		return WebhookResult{}, err
	}
	state, rawState, ok := firstTerminal(fields, "event", "status", "state", "type")
	if !ok {
		return WebhookResult{}, invalid("SABnzbd payload is not a completion or failure event")
	}
	jobID := firstField(fields, "nzo_id", "nzoid", "job_id", "jobid", "nzb_id", "nzbid", "id")
	name := firstField(fields, "nzb_name", "nzbname", "filename", "file_name", "name", "release", "title")
	category := firstField(fields, "category", "cat", "nzb_category", "nzbcategory")
	reason := firstField(fields, "fail_msg", "failmsg", "failure_reason", "error_message", "message", "err", "error")
	return finishWebhook(WebhookProviderSABnzbd, jobID, name, category, state, rawState, reason)
}

// ParseNZBGet parses a bounded NZBGet completion/failure webhook. It accepts the
// documented post-processing fields (NZBPP_NZBNAME, NZBPP_CATEGORY,
// NZBPP_TOTALSTATUS, NZBPP_STATUS and NZBID/NZBName) as JSON or form data, plus
// the lowercase variants used by community hooks. A WARNING result is treated as
// failed so a damaged download is never auto-approved.
func ParseNZBGet(body []byte) (WebhookResult, error) {
	fields, err := parseWebhookFields(body)
	if err != nil {
		return WebhookResult{}, err
	}
	state, rawState, ok := firstTerminal(fields,
		"nzbpp_totalstatus", "nzbpptotalstatus", "nzbpp_status", "nzbppstatus",
		"status", "state", "event", "result")
	if !ok {
		return WebhookResult{}, invalid("NZBGet payload is not a completion or failure event")
	}
	jobID := firstField(fields, "nzbid", "nzb_id", "job_id", "jobid", "id")
	name := firstField(fields,
		"nzbpp_nzbname", "nzbname", "nzb_name", "nzbpp_nzbfilename", "nzbfilename",
		"filename", "name", "release", "title")
	category := firstField(fields, "nzbpp_category", "nzbcategory", "nzb_category", "category", "cat")
	reason := firstField(fields, "fail_msg", "failmsg", "failure_reason", "error_message", "message", "err")
	detail := firstField(fields, "nzbpp_status", "nzbppstatus", "status", "state", "event", "result")
	if detail == "" {
		detail = rawState
	}
	return finishWebhook(WebhookProviderNZBGet, jobID, name, category, state, detail, reason)
}

func finishWebhook(provider, jobID, name, category, state, rawState, reason string) (WebhookResult, error) {
	jobID = strings.TrimSpace(jobID)
	if len(jobID) > maxIdentityBytes {
		return WebhookResult{}, invalid("webhook job identity is too long")
	}
	if strings.IndexFunc(jobID, unicode.IsControl) >= 0 {
		return WebhookResult{}, invalid("webhook job identity contains control characters")
	}
	name = cleanText(name, maxIdentityBytes)
	if jobID == "" && name == "" {
		return WebhookResult{}, invalid("completion webhook did not identify a release")
	}
	if state == StateFailed {
		reason = cleanText(reason, maxIdentityBytes)
		if reason == "" {
			switch strings.ToLower(strings.TrimSpace(rawState)) {
			case "", "failed", "failure", "error":
			default:
				reason = cleanText(rawState, maxIdentityBytes)
			}
		}
	}
	return WebhookResult{
		Provider:      provider,
		ReleaseName:   name,
		JobID:         jobID,
		Category:      cleanText(category, 128),
		State:         state,
		FailureReason: reason,
	}, nil
}

func parseWebhookFields(body []byte) (map[string]string, error) {
	if len(body) > MaxWebhookBody {
		return nil, invalid("webhook body exceeds %d bytes", MaxWebhookBody)
	}
	trimmed := bytes.TrimPrefix(bytes.TrimSpace(body), []byte{0xEF, 0xBB, 0xBF})
	trimmed = bytes.TrimSpace(trimmed)
	if len(trimmed) == 0 {
		return nil, invalid("webhook body is empty")
	}
	switch trimmed[0] {
	case '{':
		return parseJSONWebhook(trimmed)
	case '[':
		return nil, invalid("webhook body must be an object, not an array")
	}
	values, err := url.ParseQuery(string(trimmed))
	if err != nil {
		return nil, invalid("webhook body is not valid form data")
	}
	fields := make(map[string]string, len(values))
	for key, list := range values {
		if len(list) == 0 {
			continue
		}
		fields[strings.ToLower(strings.TrimSpace(key))] = list[0]
	}
	return fields, nil
}

func parseJSONWebhook(body []byte) (map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, invalid("webhook body is not valid JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, invalid("webhook JSON body has trailing data")
	}
	if raw == nil {
		return nil, invalid("webhook body must be a JSON object")
	}
	fields := make(map[string]string, len(raw))
	for key, value := range raw {
		if text := scalarString(value); text != "" {
			fields[strings.ToLower(strings.TrimSpace(key))] = text
		}
	}
	return fields, nil
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
}

func firstField(fields map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fields[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstTerminal(fields map[string]string, keys ...string) (string, string, bool) {
	for _, key := range keys {
		value := strings.TrimSpace(fields[key])
		if value == "" {
			continue
		}
		if state, ok := terminalState(value); ok {
			return state, value, true
		}
	}
	return "", "", false
}

// terminalState recognizes the completion/failure vocabularies used by SABnzbd
// and NZBGet. NZBGet's status can include a detail suffix (SUCCESS/ALL,
// WARNING/SPACE); only the first component determines the state. WARNING is
// deliberately treated as failed rather than completed.
func terminalState(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if index := strings.IndexByte(normalized, '/'); index >= 0 {
		normalized = strings.TrimSpace(normalized[:index])
	}
	switch normalized {
	case "complete", "completed", "success", "succeeded", "ok":
		return StateCompleted, true
	case "failed", "failure", "error", "warning", "deleted":
		return StateFailed, true
	default:
		return "", false
	}
}

func cleanText(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	value = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	return truncateRunes(strings.TrimSpace(value), limit)
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	count := 0
	for index := range value {
		if count == limit {
			return value[:index]
		}
		count++
	}
	return value
}
