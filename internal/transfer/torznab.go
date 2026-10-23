package transfer

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Torznab struct{ HTTP *http.Client }

func NewTorznab() *Torznab { return &Torznab{HTTP: newHTTP()} }

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
	req.Header.Set("Accept", "application/rss+xml, application/xml, application/x-bittorrent")
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return nil, unavailable("The Torznab indexer could not be reached. Check its URL and network connection.")
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
func (t *Torznab) capabilities(ctx context.Context, c IndexerConfig) (int, error) {
	u, err := indexerURL(c, "caps")
	if err != nil {
		return 0, err
	}
	body, err := t.get(ctx, c, u, 1<<20)
	if err != nil {
		return 0, err
	}
	var out caps
	if xml.Unmarshal(body, &out) != nil || out.XMLName.Local != "caps" {
		return 0, problem("indexer_response", "The URL did not return Torznab capabilities. Enter the complete API endpoint.")
	}
	if out.Search.Available != "yes" || !containsPart(out.Search.Params, "q") {
		return 0, problem("indexer_capabilities", "This Torznab endpoint must support text searches (search with q).")
	}
	limit := 50
	if out.Limits.Max > 0 && out.Limits.Max < limit {
		limit = out.Limits.Max
	}
	return limit, nil
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
	q.Set("q", query)
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
		v := Candidate{Release: Release{Title: item.Title, Size: item.Enclosure.Length}, URL: item.Enclosure.URL, Generation: c.Generation}
		if v.URL == "" {
			v.URL = item.Link
		}
		if strings.HasPrefix(v.URL, "magnet:") {
			v.Magnet = v.URL
			v.URL = ""
		}
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
			}
		}
		if v.Size < 0 {
			continue
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
		if d, e := http.ParseTime(item.Date); e == nil {
			v.Published = d.UTC().Format(time.RFC3339)
		}
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
