package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

type directCache struct {
	at    time.Time
	items []Candidate
}
type Direct struct {
	Kind  string
	HTTP  *http.Client
	mu    sync.Mutex
	next  time.Time
	cache map[[32]byte]directCache
}

func NewDirect(kind string) *Direct {
	return &Direct{Kind: kind, HTTP: newHTTP(), cache: map[[32]byte]directCache{}}
}
func sameOrigin(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && strings.EqualFold(a.Host, b.Host) && b.User == nil && b.Fragment == ""
}
func (d *Direct) request(ctx context.Context, c IndexerConfig, method string, target *url.URL, form url.Values, max int64) ([]byte, error) {
	base, e := endpoint(c.URL)
	if e != nil {
		return nil, e
	}
	if !sameOrigin(base, target) {
		return nil, problem("unsafe_release_url", "The tracker returned a link outside its configured website.")
	}
	d.mu.Lock()
	start := time.Now()
	if d.next.After(start) {
		start = d.next
	}
	d.next = start.Add(time.Second)
	d.mu.Unlock()
	if wait := time.Until(start); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, unavailable("The tracker request timed out.")
		case <-timer.C:
		}
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, e := http.NewRequestWithContext(ctx, method, target.String(), body)
	if e != nil {
		return nil, problem("invalid_response", "The tracker returned an invalid address.")
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/html, application/x-bittorrent")
	agent := c.UserAgent
	if agent == "" {
		agent = "ReelScarey/0.9"
	}
	req.Header.Set("User-Agent", agent)
	if c.Cookie != "" {
		req.Header.Set("Cookie", c.Cookie)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	// Never accept a caller-supplied redirect policy or forward a session elsewhere.
	client := *d.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, e := client.Do(req)
	if e != nil {
		return nil, unavailable("The tracker could not be reached. Check its website address and server network access.")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		d.mu.Lock()
		d.next = time.Now().Add(time.Minute)
		d.mu.Unlock()
		return nil, unavailable("The tracker is rate limiting requests. Wait before trying again.")
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 || (resp.StatusCode >= 300 && resp.StatusCode < 400) {
		return nil, problem("indexer_auth", "The tracker returned an authentication challenge or redirect. Sign in in your browser, complete any challenge, then re-copy the cookie and matching user agent from the exact configured host. Redirects are not followed automatically.")
	}
	if resp.StatusCode != 200 {
		return nil, unavailable("The tracker did not return a successful response.")
	}
	return bounded(resp.Body, max)
}
func (d *Direct) Test(ctx context.Context, c IndexerConfig) error {
	// A connection test must reach the provider even after a cached search.
	c.Generation = ID()
	_, e := d.Search(ctx, c, "")
	return e
}
func (d *Direct) Search(ctx context.Context, c IndexerConfig, query string) ([]Candidate, error) {
	key := sha256.Sum256([]byte(c.Generation + "\x00" + c.URL + "\x00" + c.Cookie + "\x00" + c.FeedURL + "\x00" + c.UserAgent + "\x00" + c.Category + "\x00" + strconv.FormatBool(c.FreeleechOnly) + "\x00" + query))
	d.mu.Lock()
	cached, ok := d.cache[key]
	d.mu.Unlock()
	if ok && time.Since(cached.at) < time.Minute {
		return append([]Candidate(nil), cached.items...), nil
	}
	base, e := endpoint(c.URL)
	if e != nil {
		return nil, e
	}
	target := *base
	target.Path = "/"
	method := http.MethodGet
	var form url.Values
	switch d.Kind {
	case "iptorrents":
		target.Path = "/t"
		q := url.Values{"qf": {"ti"}, "o": {"seeders"}}
		if query != "" {
			q.Set("q", query)
		}
		if c.Category == "5000" {
			q.Set("73", "")
		} else {
			q.Set("72", "")
		}
		if c.FreeleechOnly {
			q.Set("free", "on")
		}
		target.RawQuery = q.Encode()
	case "xspeeds":
		target.Path = "/browse.php"
		method = http.MethodPost
		form = url.Values{"do": {"search"}, "category": {"0"}, "include_dead_torrents": {"yes"}, "sort": {"seeders"}, "order": {"desc"}}
		if query != "" {
			form.Set("keywords", query)
			form.Set("search_type", "t_name")
		}
		if c.FreeleechOnly {
			form.Set("sort", "free")
		}
	case "nyaa":
		target.RawQuery = url.Values{"page": {"rss"}, "q": {query}, "c": {"0_0"}, "f": {"0"}}.Encode()
	case "rss":
		p, e := validPrivateURL(c.FeedURL)
		if e != nil {
			return nil, e
		}
		target = *p
	default:
		return nil, problem("invalid_settings", "The direct tracker type is unsupported.")
	}
	body, e := d.request(ctx, c, method, &target, form, 2<<20)
	if e != nil {
		return nil, e
	}
	var items []Candidate
	if d.Kind == "iptorrents" || d.Kind == "xspeeds" {
		items, e = parseTrackerHTML(body, base, d.Kind, c.FreeleechOnly)
	} else {
		items, e = parseDirectRSS(body, base)
	}
	if e != nil {
		return nil, e
	}
	if query != "" {
		filtered := []Candidate{}
		for _, v := range items {
			if matchesWords(v.Title, query) {
				filtered = append(filtered, v)
			}
		}
		items = filtered
	}
	d.mu.Lock()
	if len(d.cache) >= 32 {
		d.cache = map[[32]byte]directCache{}
	}
	d.cache[key] = directCache{time.Now(), append([]Candidate(nil), items...)}
	d.mu.Unlock()
	return items, nil
}
func matchesWords(title, query string) bool {
	normalize := func(s string) string {
		return strings.ToLower(regexp.MustCompile(`[^\pL\pN]+`).ReplaceAllString(s, " "))
	}
	words := strings.Fields(normalize(title))
	lookup := map[string]bool{}
	for _, w := range words {
		lookup[w] = true
	}
	for _, w := range strings.Fields(normalize(query)) {
		if !lookup[w] {
			return false
		}
	}
	return true
}
func (d *Direct) Fetch(ctx context.Context, c IndexerConfig, v Candidate) (Payload, string, error) {
	if v.Magnet != "" {
		hash, e := MagnetHash(v.Magnet)
		return Payload{Magnet: v.Magnet}, hash, e
	}
	u, e := url.Parse(v.URL)
	if e != nil {
		return Payload{}, "", problem("invalid_release", "This release has an invalid torrent address.")
	}
	raw, e := d.request(ctx, c, http.MethodGet, u, nil, 4<<20)
	if e != nil {
		return Payload{}, "", e
	}
	hash, e := TorrentHash(raw)
	if e != nil {
		return Payload{}, "", e
	}
	if v.Hash != "" && v.Hash != hash {
		return Payload{}, "", problem("torrent_hash_mismatch", "The torrent metadata no longer matches this release.")
	}
	return Payload{Torrent: raw}, hash, nil
}
func releaseLink(base *url.URL, raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "magnet:") {
		if _, e := MagnetHash(raw); e == nil {
			return "", raw, true
		}
		return "", "", false
	}
	u, e := url.Parse(raw)
	if e != nil || raw == "" || len(raw) > 8192 || strings.ContainsAny(raw, "\r\n\\\x00") {
		return "", "", false
	}
	u = base.ResolveReference(u)
	if !sameOrigin(base, u) {
		return "", "", false
	}
	return u.String(), "", true
}
func parseDirectRSS(body []byte, base *url.URL) ([]Candidate, error) {
	var rss struct {
		XMLName xml.Name
		Channel *struct {
			Items []struct {
				Title     string `xml:"title"`
				Link      string `xml:"link"`
				Date      string `xml:"pubDate"`
				Hash      string `xml:"infoHash"`
				Size      string `xml:"size"`
				Seeders   string `xml:"seeders"`
				Leechers  string `xml:"leechers"`
				Enclosure struct {
					URL    string `xml:"url,attr"`
					Length int64  `xml:"length,attr"`
				} `xml:"enclosure"`
				Attrs []attribute `xml:"attr"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if e := xml.Unmarshal(body, &rss); e != nil || rss.XMLName.Local != "rss" || rss.Channel == nil || len(rss.Channel.Items) > 1000 {
		return nil, problem("indexer_response", "The website did not return a readable torrent RSS feed.")
	}
	out := []Candidate{}
	seen := map[string]bool{}
	for _, item := range rss.Channel.Items {
		if !textOK(item.Title, 500) {
			continue
		}
		link := item.Enclosure.URL
		if link == "" {
			link = item.Link
		}
		raw, magnet, ok := releaseLink(base, link)
		if !ok || seen[raw+magnet] {
			continue
		}
		seen[raw+magnet] = true
		v := Candidate{Release: Release{Title: item.Title, Size: max(0, item.Enclosure.Length), Seeders: nonnegative(strings.TrimSpace(item.Seeders))}, URL: raw, Magnet: magnet, Hash: strings.ToLower(strings.TrimSpace(item.Hash))}
		if v.Size == 0 {
			v.Size = humanSize(item.Size)
		}
		if leech := nonnegative(strings.TrimSpace(item.Leechers)); leech != nil && v.Seeders != nil {
			peers := *leech + *v.Seeders
			v.Peers = &peers
		}
		for _, a := range item.Attrs {
			switch strings.ToLower(a.Name) {
			case "size":
				if n, e := strconv.ParseInt(a.Value, 10, 64); e == nil && n >= 0 {
					v.Size = n
				}
			case "seeders":
				v.Seeders = nonnegative(a.Value)
			case "peers":
				v.Peers = nonnegative(a.Value)
			case "infohash":
				v.Hash = strings.ToLower(a.Value)
			}
		}
		if v.Hash != "" {
			b, e := hex.DecodeString(v.Hash)
			if e != nil || len(b) != 20 {
				continue
			}
		}
		if v.Magnet != "" {
			h, _ := MagnetHash(v.Magnet)
			if v.Hash != "" && h != v.Hash {
				continue
			}
			v.Hash = h
		}
		if parsed, e := http.ParseTime(item.Date); e == nil {
			v.Published = parsed.UTC().Format(time.RFC3339)
		}
		out = append(out, v)
		if len(out) == 100 {
			break
		}
	}
	return out, nil
}

var sizePattern = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(KiB|MiB|GiB|TiB|KB|MB|GB|TB|B)\b`)

func humanSize(s string) int64 {
	m := sizePattern.FindStringSubmatch(strings.ReplaceAll(s, ",", ""))
	if len(m) != 3 {
		return 0
	}
	n, e := strconv.ParseFloat(m[1], 64)
	if e != nil {
		return 0
	}
	power := 0
	switch strings.ToUpper(m[2][:1]) {
	case "K":
		power = 1
	case "M":
		power = 2
	case "G":
		power = 3
	case "T":
		power = 4
	}
	n *= math.Pow(1024, float64(power))
	if n <= 0 || n >= math.MaxInt64 {
		return 0
	}
	return int64(math.Round(n))
}
func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}
func nodeText(n *html.Node) string {
	var b strings.Builder
	for d := range n.Descendants() {
		if d.Type == html.TextNode {
			b.WriteString(d.Data)
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
func parseTrackerHTML(body []byte, base *url.URL, kind string, freeOnly bool) ([]Candidate, error) {
	doc, e := html.Parse(bytes.NewReader(body))
	if e != nil {
		return nil, problem("indexer_response", "The tracker page could not be read.")
	}
	var table *html.Node
	logged := false
	expected := "torrents"
	if kind == "xspeeds" {
		expected = "sortabletable"
	}
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if n.Data == "a" {
			href := attr(n, "href")
			if strings.Contains(href, "/lout.php") || strings.Contains(href, "logout.php") {
				logged = true
			}
		}
		if n.Data == "table" && attr(n, "id") == expected {
			table = n
		}
	}
	if table == nil {
		if logged {
			return nil, problem("indexer_layout", "The signed-in tracker page was reached, but its torrent table layout is not recognized. For IPTorrents, use Icons for categories and show the files-count column in tracker settings.")
		}
		return nil, problem("indexer_auth", "The tracker did not show a signed-in page. Copy the cookie from a request to the exact configured host; tracker sessions can be bound to your public IP, and browser challenges must be completed first.")
	}
	seedCol, peerCol := -1, -1
	headers := []string{}
	for n := range table.Descendants() {
		if n.Type == html.ElementNode && n.Data == "th" {
			label := nodeText(n) + " " + attr(n, "title")
			for child := range n.Descendants() {
				label += " " + attr(child, "title")
			}
			headers = append(headers, strings.ToLower(label))
		}
	}
	for i, h := range headers {
		if strings.Contains(h, "seed") {
			seedCol = i
		}
		if strings.Contains(h, "leech") || strings.Contains(h, "peer") {
			peerCol = i
		}
	}
	out := []Candidate{}
	seen := map[string]bool{}
	for row := range table.Descendants() {
		if row.Type != html.ElementNode || row.Data != "tr" {
			continue
		}
		var title, download string
		free := false
		cells := []*html.Node{}
		for n := row.FirstChild; n != nil; n = n.NextSibling {
			if n.Type == html.ElementNode && n.Data == "td" {
				cells = append(cells, n)
			}
		}
		for n := range row.Descendants() {
			if n.Type != html.ElementNode {
				continue
			}
			if n.Data == "span" && strings.Contains(" "+attr(n, "class")+" ", " free ") {
				free = true
			}
			if n.Data == "img" && (strings.HasPrefix(attr(n, "alt"), "Free Torrent") || strings.HasPrefix(attr(n, "alt"), "Sitewide Free Torrent")) {
				free = true
			}
			if n.Data != "a" {
				continue
			}
			href := attr(n, "href")
			if (kind == "iptorrents" && strings.Contains(" "+attr(n, "class")+" ", " hv ")) || (kind == "xspeeds" && strings.Contains(href, "details.php?id=")) {
				if title == "" {
					title = nodeText(n)
				}
			}
			if strings.Contains(href, "download.php") {
				download = href
			}
		}
		if !textOK(title, 500) || download == "" || (freeOnly && !free) {
			continue
		}
		link, _, ok := releaseLink(base, download)
		if !ok || seen[link] {
			continue
		}
		u, _ := url.Parse(link)
		if !strings.HasPrefix(u.Path, "/download.php") {
			continue
		}
		seen[link] = true
		v := Candidate{Release: Release{Title: title, Freeleech: free}, URL: link}
		for _, cell := range cells {
			if size := humanSize(nodeText(cell)); size > 0 {
				v.Size = size
				break
			}
		}
		if kind == "xspeeds" && seedCol < 0 && len(cells) >= 8 {
			seedCol = 6
			peerCol = 7
		}
		if seedCol >= 0 && seedCol < len(cells) {
			v.Seeders = nonnegative(strings.ReplaceAll(nodeText(cells[seedCol]), ",", ""))
		}
		if peerCol >= 0 && peerCol < len(cells) {
			leech := nonnegative(strings.ReplaceAll(nodeText(cells[peerCol]), ",", ""))
			if leech != nil && v.Seeders != nil {
				total := *v.Seeders + *leech
				v.Peers = &total
			}
		}
		out = append(out, v)
		if len(out) == 100 {
			break
		}
	}
	return out, nil
}
