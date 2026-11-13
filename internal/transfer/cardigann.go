package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	stdhtml "html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/cascadia"
	xhtml "golang.org/x/net/html"
	"gopkg.in/yaml.v3"
)

// Cardigann definitions are untrusted configuration. This adapter implements a
// conservative Cardigann subset: HTML/CSS search rows, common text filters,
// cookie-based private sessions and torrent or magnet download links. It never
// executes definition code, follows redirects or fetches arbitrary user URLs.
type cardigannDefinition struct {
	ID          string             `yaml:"id"`
	Name        string             `yaml:"name"`
	Description string             `yaml:"description"`
	Type        string             `yaml:"type"`
	Links       []string           `yaml:"links"`
	Settings    []cardigannSetting `yaml:"settings"`
	Login       *cardigannLogin    `yaml:"login"`
	Search      cardigannSearch    `yaml:"search"`
	Download    *cardigannDownload `yaml:"download"`
}

type cardigannSetting struct {
	Name    string `yaml:"name"`
	Label   string `yaml:"label"`
	Type    string `yaml:"type"`
	Default any    `yaml:"default"`
}

type cardigannLogin struct {
	Path   string            `yaml:"path"`
	Method string            `yaml:"method"`
	Inputs map[string]string `yaml:"inputs"`
	Form   string            `yaml:"form"`
	Error  string            `yaml:"error"`
}

type cardigannSearch struct {
	Path   string                    `yaml:"path"`
	Inputs map[string]string         `yaml:"inputs"`
	Rows   cardigannRows             `yaml:"rows"`
	Fields map[string]cardigannField `yaml:"fields"`
}

type cardigannRows struct {
	Selector  string            `yaml:"selector"`
	Attribute string            `yaml:"attribute"`
	Filters   []cardigannFilter `yaml:"filters"`
}

type cardigannField struct {
	Selector  string            `yaml:"selector"`
	Attribute string            `yaml:"attribute"`
	Default   string            `yaml:"default"`
	Optional  bool              `yaml:"optional"`
	Remove    string            `yaml:"remove"`
	Filters   []cardigannFilter `yaml:"filters"`
}

type cardigannFilter struct {
	Name string `yaml:"name"`
	Args any    `yaml:"args"`
}

type cardigannDownload struct {
	Selector  string            `yaml:"selector"`
	Attribute string            `yaml:"attribute"`
	Filters   []cardigannFilter `yaml:"filters"`
	Method    string            `yaml:"method"`
}

type Cardigann struct {
	HTTP *http.Client
}

var (
	cardigannSelectorCache sync.Map
	cardigannRegexCache    sync.Map
)

func cardigannSelector(selector string) (cascadia.Sel, error) {
	if cached, ok := cardigannSelectorCache.Load(selector); ok {
		return cached.(cascadia.Sel), nil
	}
	parsed, err := cascadia.Parse(selector)
	if err == nil {
		cardigannSelectorCache.Store(selector, parsed)
	}
	return parsed, err
}

func cardigannRegexp(pattern string) (*regexp.Regexp, error) {
	if cached, ok := cardigannRegexCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	compiled, err := regexp.Compile(pattern)
	if err == nil {
		cardigannRegexCache.Store(pattern, compiled)
	}
	return compiled, err
}

func NewCardigann() *Cardigann { return &Cardigann{HTTP: newHTTP()} }

func (c *Cardigann) definition(config IndexerConfig) (cardigannDefinition, error) {
	var def cardigannDefinition
	if strings.TrimSpace(config.Definition) == "" {
		return def, problem("invalid_settings", "Paste a Cardigann YAML definition.")
	}
	if len(config.Definition) > 200_000 {
		return def, problem("invalid_settings", "The Cardigann definition is too large.")
	}
	decoder := yaml.NewDecoder(bytes.NewReader([]byte(config.Definition)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&def); err != nil {
		return def, problem("invalid_settings", "The Cardigann definition is not valid YAML or contains unsupported fields.")
	}
	if strings.TrimSpace(def.Name) == "" || strings.TrimSpace(def.ID) == "" {
		return def, problem("invalid_settings", "The Cardigann definition needs an id and name.")
	}
	if def.Login != nil {
		return def, problem("invalid_settings", "Cardigann login blocks are not supported. Enter the tracker session cookie instead.")
	}
	if strings.TrimSpace(def.Search.Rows.Selector) == "" || len(def.Search.Fields) == 0 {
		return def, problem("invalid_settings", "The Cardigann definition needs search rows and fields.")
	}
	if _, ok := def.Search.Fields["title"]; !ok {
		return def, problem("invalid_settings", "The Cardigann definition needs a title field.")
	}
	if _, ok := def.Search.Fields["download"]; !ok {
		if _, magnet := def.Search.Fields["magnet"]; !magnet {
			return def, problem("invalid_settings", "The Cardigann definition needs a download or magnet field.")
		}
	}
	return def, nil
}

func (c *Cardigann) Test(ctx context.Context, config IndexerConfig) error {
	config.Generation = ID()
	_, err := c.Search(ctx, config, "test")
	return err
}

func (c *Cardigann) Recent(ctx context.Context, config IndexerConfig) ([]Candidate, error) {
	return c.Search(ctx, config, "")
}

func (c *Cardigann) Search(ctx context.Context, config IndexerConfig, query string) ([]Candidate, error) {
	def, err := c.definition(config)
	if err != nil {
		return nil, err
	}
	base, err := endpoint(config.URL)
	if err != nil {
		return nil, err
	}
	target, err := cardigannTarget(base, def, config, query)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, unavailable("The indexer request could not be prepared.")
	}
	cardigannHeaders(req, config)
	response, err := c.HTTP.Do(req)
	if err != nil {
		return nil, unavailable("The Cardigann indexer could not be reached.")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden {
		return nil, problem("indexer_refused", "The Cardigann indexer refused the request. Check the session cookie, user agent and rate limit.")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		// A rate limit is temporary, not a permanent refusal. Honor the
		// provider's Retry-After hint so the worker waits instead of hammering.
		e := &Error{Code: "upstream_unavailable", Message: "The Cardigann indexer is limiting requests. Try again later.", Temporary: true}
		e.RetryAt = retryAfterSeconds(response.Header.Get("Retry-After"))
		return nil, e
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, unavailable(fmt.Sprintf("The Cardigann indexer returned HTTP %d.", response.StatusCode))
	}
	body, err := readLimited(response.Body, 4<<20)
	if err != nil {
		return nil, unavailable("The Cardigann indexer returned an unreadable page.")
	}
	document, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, problem("indexer_response", "The Cardigann indexer returned malformed HTML.")
	}
	rows, err := cardigannSelector(def.Search.Rows.Selector)
	if err != nil {
		return nil, problem("invalid_settings", "The search rows selector is invalid.")
	}
	nodes := cascadia.QueryAll(document, rows)
	if len(nodes) > 100 {
		nodes = nodes[:100]
	}
	out := make([]Candidate, 0, len(nodes))
	for _, node := range nodes {
		title, _ := cardigannValue(node, def.Search.Fields["title"], config)
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		rawDownload, _ := cardigannValue(node, fieldOr(def.Search.Fields["download"], def.Search.Fields["magnet"]), config)
		details, _ := cardigannValue(node, def.Search.Fields["details"], config)
		sizeText, _ := cardigannValue(node, def.Search.Fields["size"], config)
		seedersText, _ := cardigannValue(node, def.Search.Fields["seeders"], config)
		leechersText, _ := cardigannValue(node, def.Search.Fields["leechers"], config)
		dateText, _ := cardigannValue(node, def.Search.Fields["date"], config)
		category, _ := cardigannValue(node, def.Search.Fields["category"], config)
		if category == "" {
			category = config.Category
		}
		download := strings.TrimSpace(stdhtml.UnescapeString(rawDownload))
		candidate := Candidate{
			Release: Release{
				Title:     title,
				Size:      humanSize(sizeText),
				Seeders:   parseCardigannInt(seedersText),
				Peers:     parseCardigannInt(leechersText),
				Published: parseCardigannDate(dateText),
				InfoURL:   safeCardigannURL(details, base),
			},
			Generation: config.Generation,
		}
		if strings.HasPrefix(strings.ToLower(download), "magnet:") {
			candidate.Magnet = download
		} else if download != "" {
			candidate.URL = safeCardigannURL(download, base)
		}
		if candidate.URL == "" && candidate.Magnet == "" {
			continue
		}
		if candidate.InfoURL == "" && candidate.URL != "" && !strings.HasPrefix(strings.ToLower(candidate.URL), "magnet:") {
			// A tracker details page is not synthesized from a download URL.
			candidate.InfoURL = ""
		}
		candidate.ID = cardigannCandidateID(title, candidate.URL, candidate.Magnet)
		out = append(out, candidate)
	}
	return out, nil
}

func fieldOr(value, fallback cardigannField) cardigannField {
	if value.Selector != "" || value.Attribute != "" || len(value.Filters) > 0 || value.Default != "" {
		return value
	}
	return fallback
}

func cardigannTarget(base *url.URL, def cardigannDefinition, config IndexerConfig, query string) (*url.URL, error) {
	target := *base
	pathText := strings.TrimSpace(def.Search.Path)
	pathText = replaceCardigann(pathText, def, config, query)
	if pathText != "" {
		if strings.HasPrefix(pathText, "http://") || strings.HasPrefix(pathText, "https://") {
			parsed, err := url.Parse(pathText)
			if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Host, base.Host) || parsed.User != nil {
				return nil, problem("invalid_settings", "The Cardigann search path must stay on the configured host.")
			}
			target = *parsed
		} else {
			if strings.Contains(pathText, "?") {
				parsed, err := url.Parse(pathText)
				if err != nil {
					return nil, problem("invalid_settings", "The Cardigann search path is invalid.")
				}
				target.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(parsed.Path, "/")
				target.RawQuery = parsed.RawQuery
			} else {
				target.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(pathText, "/")
				target.RawQuery = ""
			}
		}
	}
	if !strings.EqualFold(target.Host, base.Host) || target.User != nil {
		return nil, problem("invalid_settings", "The Cardigann search path must stay on the configured host.")
	}
	values := target.Query()
	for key, value := range def.Search.Inputs {
		values.Set(key, replaceCardigann(value, def, config, query))
	}
	target.RawQuery = values.Encode()
	target.Fragment = ""
	return &target, nil
}

func replaceCardigann(value string, def cardigannDefinition, config IndexerConfig, query string) string {
	settings := map[string]string{}
	for _, setting := range def.Settings {
		settings[setting.Name] = fmt.Sprint(setting.Default)
	}
	for name, value := range config.Settings {
		settings[name] = value
	}
	value = strings.ReplaceAll(value, "{{ .Keywords }}", url.QueryEscape(query))
	value = strings.ReplaceAll(value, "{{.Keywords}}", url.QueryEscape(query))
	value = strings.ReplaceAll(value, "{{ .Query }}", url.QueryEscape(query))
	value = strings.ReplaceAll(value, "{{.Query}}", url.QueryEscape(query))
	value = strings.ReplaceAll(value, "{{ .Category }}", url.QueryEscape(config.Category))
	for name, setting := range settings {
		value = strings.ReplaceAll(value, "{{ .Settings."+name+" }}", url.QueryEscape(setting))
	}
	value = strings.ReplaceAll(value, "{{ .Config.api_key }}", url.QueryEscape(config.APIKey))
	value = strings.ReplaceAll(value, "{{ .Config.cookie }}", url.QueryEscape(config.Cookie))
	value = strings.ReplaceAll(value, "{{ .Config.user_agent }}", url.QueryEscape(config.UserAgent))
	return value
}

func cardigannHeaders(req *http.Request, config IndexerConfig) {
	agent := strings.TrimSpace(config.UserAgent)
	if agent == "" {
		agent = "Mozilla/5.0 (compatible; ReelScarey)"
	}
	req.Header.Set("User-Agent", agent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	if cookie := strings.TrimSpace(config.Cookie); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if apiKey := strings.TrimSpace(config.APIKey); apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
	}
}

func cardigannValue(row *xhtml.Node, field cardigannField, config IndexerConfig) (string, bool) {
	if field.Selector == "" && field.Attribute == "" {
		return field.Default, field.Default != ""
	}
	selector, err := cardigannSelector(field.Selector)
	if err != nil {
		return field.Default, false
	}
	node := cascadia.Query(row, selector)
	if node == nil {
		return field.Default, field.Optional
	}
	value := ""
	if field.Attribute != "" {
		for _, attr := range node.Attr {
			if strings.EqualFold(attr.Key, field.Attribute) {
				value = attr.Val
				break
			}
		}
	} else {
		value = textContent(node)
	}
	for _, filter := range field.Filters {
		value = applyCardigannFilter(value, filter)
	}
	return strings.TrimSpace(value), true
}

func textContent(node *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node, int)
	walk = func(n *xhtml.Node, depth int) {
		if depth > 200 {
			return
		}
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
			return
		}
		if n.Type == xhtml.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, depth+1)
		}
	}
	walk(node, 0)
	return strings.Join(strings.Fields(b.String()), " ")
}

func applyCardigannFilter(value string, filter cardigannFilter) string {
	name := strings.ToLower(strings.TrimSpace(filter.Name))
	args := fmt.Sprint(filter.Args)
	switch name {
	case "trim":
		return strings.TrimSpace(value)
	case "tolower", "lower":
		return strings.ToLower(value)
	case "toupper", "upper":
		return strings.ToUpper(value)
	case "htmldecode":
		return stdhtml.UnescapeString(value)
	case "urldecode":
		if decoded, err := url.QueryUnescape(value); err == nil {
			return decoded
		}
	case "base64":
		if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
			return string(decoded)
		}
	case "trimprefix":
		return strings.TrimPrefix(value, args)
	case "trimsuffix":
		return strings.TrimSuffix(value, args)
	case "prepend":
		return args + value
	case "append":
		return value + args
	case "replace":
		parts := splitArgs(args)
		if len(parts) >= 2 {
			return strings.ReplaceAll(value, parts[0], parts[1])
		}
	case "regexp", "re_replace":
		parts := splitArgs(args)
		if len(parts) >= 2 {
			if re, err := cardigannRegexp(parts[0]); err == nil {
				return re.ReplaceAllString(value, parts[1])
			}
		}
	case "split":
		parts := splitArgs(args)
		if len(parts) >= 1 {
			fields := strings.Split(value, parts[0])
			if len(parts) >= 2 {
				if index, err := strconv.Atoi(parts[1]); err == nil && index >= 0 && index < len(fields) {
					return fields[index]
				}
			}
			return strings.Join(fields, " ")
		}
	}
	return value
}

func splitArgs(value string) []string {
	if strings.Contains(value, "|") {
		return strings.Split(value, "|")
	}
	return strings.Split(value, ",")
}

func readLimited(reader io.Reader, maximum int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("response exceeds %d bytes", maximum)
	}
	return data, nil
}

func parseCardigannInt(value string) *int {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	if value == "" {
		return nil
	}
	if index := strings.IndexAny(value, " \t"); index > 0 {
		value = value[:index]
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return nil
	}
	return &number
}

func parseCardigannDate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02", "02 Jan 2006", "Jan 2 2006"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func safeCardigannURL(raw string, base *url.URL) string {
	raw = strings.TrimSpace(stdhtml.UnescapeString(raw))
	if raw == "" || strings.HasPrefix(strings.ToLower(raw), "magnet:") {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	resolved := base.ResolveReference(parsed)
	if !strings.EqualFold(resolved.Host, base.Host) || (resolved.Scheme != "http" && resolved.Scheme != "https") {
		return ""
	}
	return resolved.String()
}

func cardigannCandidateID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func (c *Cardigann) Fetch(ctx context.Context, config IndexerConfig, candidate Candidate) (Payload, string, error) {
	if strings.HasPrefix(strings.ToLower(candidate.Magnet), "magnet:") {
		hash, err := MagnetHash(candidate.Magnet)
		if err != nil {
			return Payload{}, "", err
		}
		return Payload{Magnet: candidate.Magnet}, hash, nil
	}
	if candidate.URL == "" {
		return Payload{}, "", problem("invalid_release", "This Cardigann release has no download link.")
	}
	base, err := endpoint(config.URL)
	if err != nil {
		return Payload{}, "", err
	}
	target := safeCardigannURL(candidate.URL, base)
	if target == "" {
		return Payload{}, "", problem("invalid_release", "This Cardigann release has an unsafe download address.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Payload{}, "", unavailable("The torrent download could not be prepared.")
	}
	cardigannHeaders(req, config)
	response, err := c.HTTP.Do(req)
	if err != nil {
		return Payload{}, "", unavailable("The torrent download could not be reached.")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Payload{}, "", unavailable(fmt.Sprintf("The torrent host returned HTTP %d.", response.StatusCode))
	}
	raw, err := readLimited(response.Body, 4<<20)
	if err != nil {
		return Payload{}, "", unavailable("The torrent download was unreadable.")
	}
	hash, err := TorrentHash(raw)
	if err != nil {
		return Payload{}, "", err
	}
	if candidate.Hash != "" && candidate.Hash != hash {
		return Payload{}, "", problem("torrent_hash_mismatch", "The torrent metadata no longer matches this release.")
	}
	return Payload{Torrent: raw}, hash, nil
}
