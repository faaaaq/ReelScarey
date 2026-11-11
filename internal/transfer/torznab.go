package transfer

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Torznab struct {
	HTTP *http.Client
	mu   sync.Mutex
	// caps caches capabilities per endpoint/generation so each search and feed
	// pass does not spend an extra round trip on an unchanging document.
	caps map[string]cachedCaps
}

type cachedCaps struct {
	value caps
	until time.Time
}

func NewTorznab() *Torznab {
	return &Torznab{HTTP: newHTTP(), caps: map[string]cachedCaps{}}
}

type feedError struct {
	Code        string `xml:"code,attr"`
	Description string `xml:"description,attr"`
}
type caps struct {
	XMLName xml.Name
	Limits  struct {
		Max int `xml:"max,attr"`
	} `xml:"limits"`
	Search struct {
		Available string `xml:"available,attr"`
		Params    string `xml:"supportedParams,attr"`
	} `xml:"searching>search"`
}
type attribute struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}
type feed struct {
	XMLName xml.Name
	Items   []struct {
		Title     string `xml:"title"`
		GUID      string `xml:"guid"`
		Link      string `xml:"link"`
		Comments  string `xml:"comments"`
		Date      string `xml:"pubDate"`
		Enclosure struct {
			URL    string `xml:"url,attr"`
			Length int64  `xml:"length,attr"`
			Type   string `xml:"type,attr"`
		} `xml:"enclosure"`
		Attrs []attribute `xml:"attr"`
	} `xml:"channel>item"`
}

func bounded(r io.Reader, max int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil || int64(len(body)) > max {
		return nil, problem("invalid_response", "The service returned an unreadable or oversized response.")
	}
	return body, nil
}
func (t *Torznab) get(ctx context.Context, c IndexerConfig, target *url.URL, max int64) ([]byte, error) {
	base, err := endpoint(c.URL)
	if err != nil {
		return nil, err
	}
	if target.Scheme != base.Scheme || !strings.EqualFold(target.Host, base.Host) || target.User != nil || target.Fragment != "" {
		return nil, problem("unsafe_release_url", "This indexer returned a download URL outside its configured service. Use an indexer-proxied torrent or magnet link.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, problem("invalid_response", "The indexer returned an invalid URL.")
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml, application/x-bittorrent, application/x-nzb")
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return nil, unavailable("The indexer could not be reached. Check its URL and network connection.")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, problem("indexer_auth", "The indexer rejected its API key.")
	}
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return nil, unavailable("The indexer is busy or unavailable. Try again later.")
	}
	if resp.StatusCode != 200 {
		return nil, problem("indexer_response", "The indexer did not return a successful response. Redirects are not followed.")
	}
	body, err := bounded(resp.Body, max)
	if err != nil {
		return nil, err
	}
	var upstream feedError
	var root struct{ XMLName xml.Name }
	if xml.Unmarshal(body, &root) == nil && root.XMLName.Local == "error" {
		_ = xml.Unmarshal(body, &upstream)
		if upstream.Code == "100" || upstream.Code == "101" || upstream.Code == "102" {
			return nil, problem("indexer_auth", "The indexer rejected its API key.")
		}
		return nil, problem("indexer_response", "The indexer rejected this request. Check its Torznab configuration.")
	}
	return body, nil
}
func indexerURL(c IndexerConfig, kind string) (*url.URL, error) {
	u, err := endpoint(c.URL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("t", kind)
	if c.APIKey != "" {
		q.Set("apikey", c.APIKey)
	}
	u.RawQuery = q.Encode()
	return u, nil
}
func (t *Torznab) Test(ctx context.Context, c IndexerConfig) error {
	_, err := t.capabilities(ctx, c)
	return err
}
func (t *Torznab) readCaps(ctx context.Context, c IndexerConfig) (caps, error) {
	key := c.URL + "\x00" + c.Generation + "\x00" + c.APIKey
	t.mu.Lock()
	if entry, ok := t.caps[key]; ok && time.Now().Before(entry.until) {
		t.mu.Unlock()
		return entry.value, nil
	}
	t.mu.Unlock()
	out, err := t.fetchCaps(ctx, c)
	if err != nil {
		return caps{}, err
	}
	t.mu.Lock()
	if len(t.caps) >= 32 {
		t.caps = map[string]cachedCaps{}
	}
	t.caps[key] = cachedCaps{value: out, until: time.Now().Add(15 * time.Minute)}
	t.mu.Unlock()
	return out, nil
}
func (t *Torznab) fetchCaps(ctx context.Context, c IndexerConfig) (caps, error) {
	u, err := indexerURL(c, "caps")
	if err != nil {
		return caps{}, err
	}
	body, err := t.get(ctx, c, u, 1<<20)
	if err != nil {
		return caps{}, err
	}
	var out caps
	if xml.Unmarshal(body, &out) != nil || out.XMLName.Local != "caps" {
		return caps{}, problem("indexer_response", "The URL did not return indexer capabilities. Enter the complete API endpoint.")
	}
	return out, nil
}
func feedLimit(out caps) int {
	limit := 50
	if out.Limits.Max > 0 && out.Limits.Max < limit {
		limit = out.Limits.Max
	}
	return limit
}
func (t *Torznab) capabilities(ctx context.Context, c IndexerConfig) (int, error) {
	out, err := t.readCaps(ctx, c)
	if err != nil {
		return 0, err
	}
	if out.Search.Available != "yes" || !containsPart(out.Search.Params, "q") {
		return 0, problem("indexer_capabilities", "This endpoint must support text searches (search with q).")
	}
	return feedLimit(out), nil
}
func containsPart(list, part string) bool {
	for _, s := range strings.Split(list, ",") {
		if strings.TrimSpace(s) == part {
			return true
		}
	}
	return false
}
func (t *Torznab) Search(ctx context.Context, c IndexerConfig, query string) ([]Candidate, error) {
	limit, err := t.capabilities(ctx, c)
	if err != nil {
		return nil, err
	}
	u, err := indexerURL(c, "search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	providerQuery := trackerQuery(query)
	if query != "" && providerQuery == "" {
		return nil, problem("invalid_search", "Enter a release search with letters or numbers.")
	}
	if providerQuery != "" {
		q.Set("q", providerQuery)
	}
	category := c.Category
	if category == "" {
		category = "2000"
	}
	q.Set("cat", category)
	q.Set("extended", "1")
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()
	body, err := t.get(ctx, c, u, 2<<20)
	if err != nil {
		return nil, err
	}
	return t.parseFeed(c, body)
}

// Recent asks the provider for its newest releases without a title query. A
// configured category narrows the feed; otherwise the provider's full recent
// list is used and the caller matches only saved, monitored titles.
func (t *Torznab) Recent(ctx context.Context, c IndexerConfig) ([]Candidate, error) {
	out, err := t.readCaps(ctx, c)
	if err != nil {
		return nil, err
	}
	u, err := indexerURL(c, "search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("extended", "1")
	q.Set("limit", strconv.Itoa(feedLimit(out)))
	if c.Category != "" {
		q.Set("cat", c.Category)
	}
	u.RawQuery = q.Encode()
	body, err := t.get(ctx, c, u, 2<<20)
	if err != nil {
		return nil, err
	}
	return t.parseFeed(c, body)
}

func (t *Torznab) parseFeed(c IndexerConfig, body []byte) ([]Candidate, error) {
	var rss feed
	if xml.Unmarshal(body, &rss) != nil || rss.XMLName.Local != "rss" || len(rss.Items) > 200 {
		return nil, problem("indexer_response", "The indexer returned an invalid torrent search feed.")
	}
	out := make([]Candidate, 0)
	seen := map[string]bool{}
	for _, item := range rss.Items {
		if !textOK(item.Title, 500) {
			continue
		}
		v := Candidate{Release: Release{Title: item.Title, Size: item.Enclosure.Length}, URL: item.Enclosure.URL, Generation: c.Generation, Guid: item.GUID}
		if v.URL == "" {
			v.URL = item.Link
		}
		if strings.HasPrefix(v.URL, "magnet:") {
			v.Magnet = v.URL
			v.URL = ""
		}
		date := item.Date
		for _, a := range item.Attrs {
			switch strings.ToLower(a.Name) {
			case "size":
				n, e := strconv.ParseInt(a.Value, 10, 64)
				if e == nil && n >= 0 {
					v.Size = n
				}
			case "seeders":
				v.Seeders = nonnegative(a.Value)
			case "peers":
				v.Peers = nonnegative(a.Value)
			case "magneturl":
				v.Magnet = a.Value
			case "infohash":
				v.Hash = strings.ToLower(a.Value)
			case "publishdate", "pubdate", "date", "usenetdate":
				if strings.TrimSpace(date) == "" {
					date = a.Value
				}
			}
		}
		if v.Size < 0 {
			continue
		}
		if c.Kind == "newznab" {
			if v.Magnet != "" || (item.Enclosure.Type != "" && item.Enclosure.Type != "application/x-nzb") {
				continue
			}
			v.Hash = ""
			v.Seeders = nil
			v.Peers = nil
			v.Protocol = "usenet"
		}
		identity := item.GUID
		if identity == "" {
			identity = v.URL + v.Magnet
		}
		if identity == "" || seen[identity] {
			continue
		}
		seen[identity] = true
		if v.Magnet != "" {
			h, e := MagnetHash(v.Magnet)
			if e != nil {
				continue
			}
			v.Hash = h
		} else {
			link, e := url.Parse(v.URL)
			base, _ := endpoint(c.URL)
			if e != nil || link.Scheme != base.Scheme || !strings.EqualFold(link.Host, base.Host) || link.User != nil {
				continue
			}
		}
		v.Published = feedTime(date)
		v.InfoURL = releaseInfoURL(item.Comments)
		out = append(out, v)
		if len(out) == 50 {
			break
		}
	}
	return out, nil
}
func nonnegative(s string) *int {
	n, e := strconv.Atoi(s)
	if e != nil || n < 0 {
		return nil
	}
	return &n
}
func (t *Torznab) Fetch(ctx context.Context, c IndexerConfig, v Candidate) (Payload, string, error) {
	if v.Magnet != "" {
		hash, err := MagnetHash(v.Magnet)
		return Payload{Magnet: v.Magnet}, hash, err
	}
	u, err := url.Parse(v.URL)
	if err != nil {
		return Payload{}, "", problem("invalid_release", "This release has no valid torrent URL.")
	}
	body, err := t.get(ctx, c, u, 4<<20)
	if err != nil {
		return Payload{}, "", err
	}
	hash, err := TorrentHash(body)
	if err != nil {
		return Payload{}, "", err
	}
	if v.Hash != "" && v.Hash != hash {
		return Payload{}, "", problem("torrent_hash_mismatch", "The torrent metadata does not match the indexer's release hash.")
	}
	return Payload{Torrent: body}, hash, nil
}
