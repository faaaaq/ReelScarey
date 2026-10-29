package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type FranchiseMember struct {
	Kind        string `json:"kind"`
	TMDbID      int64  `json:"tmdb_id"`
	Title       string `json:"title"`
	WikidataID  string `json:"wikidata_id"`
	FranchiseID string `json:"franchise_id"`
	Franchise   string `json:"franchise"`
}
type FranchiseResults struct {
	Items     []FranchiseMember `json:"items"`
	Truncated bool              `json:"truncated"`
}
type FranchiseProvider interface {
	FindFranchise(context.Context, string, int64) (FranchiseResults, error)
}
type franchiseCache struct {
	value FranchiseResults
	until time.Time
}
type WikidataFranchises struct {
	base      string
	client    *http.Client
	mu        sync.Mutex
	cache     map[string]franchiseCache
	semaphore chan struct{}
}

func NewWikidataFranchises() *WikidataFranchises {
	return newWikidataFranchises("https://query.wikidata.org/sparql", &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects disabled") }})
}
func newWikidataFranchises(base string, c *http.Client) *WikidataFranchises {
	return &WikidataFranchises{base: base, client: c, cache: map[string]franchiseCache{}, semaphore: make(chan struct{}, 1)}
}

var wikidataURI = regexp.MustCompile(`^http://www\.wikidata\.org/entity/(Q[1-9][0-9]{0,15})$`)

func wikidataID(v string) string {
	m := wikidataURI.FindStringSubmatch(v)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}
func validLabel(v string) bool {
	return v != "" && len(v) <= 500 && utf8.ValidString(v) && strings.IndexFunc(v, unicode.IsControl) < 0
}
func (s *WikidataFranchises) FindFranchise(ctx context.Context, kind string, id int64) (FranchiseResults, error) {
	out := FranchiseResults{Items: []FranchiseMember{}}
	if id < 1 || (kind != "movie" && kind != "series") {
		return out, &Error{Code: "invalid_title", Message: "Choose a valid movie or show."}
	}
	key := kind + ":" + strconv.FormatInt(id, 10)
	s.mu.Lock()
	cached, ok := s.cache[key]
	s.mu.Unlock()
	if ok && time.Now().Before(cached.until) {
		return cached.value, nil
	}
	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	case <-ctx.Done():
		return out, ctx.Err()
	}
	s.mu.Lock()
	cached, ok = s.cache[key]
	s.mu.Unlock()
	if ok && time.Now().Before(cached.until) {
		return cached.value, nil
	}
	property := "P4947"
	if kind == "series" {
		property = "P4983"
	}
	query := fmt.Sprintf(`SELECT DISTINCT ?seed ?franchise ?franchiseLabel ?member ?memberLabel ?movie ?tv WHERE { ?seed wdt:%s "%d"; wdt:P8345 ?franchise. FILTER NOT EXISTS { ?other wdt:%s "%d". FILTER(?other != ?seed) } ?member wdt:P8345 ?franchise. { ?member wdt:P4947 ?movie } UNION { ?member wdt:P4983 ?tv } SERVICE wikibase:label { bd:serviceParam wikibase:language "en". } } LIMIT 201`, property, id, property, id)
	u, e := url.Parse(s.base)
	if e != nil {
		return out, e
	}
	u.RawQuery = url.Values{"query": {query}, "format": {"json"}}.Encode()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if e != nil {
		return out, e
	}
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", "ReelScarey/0.20 (https://github.com/faaaaq/ReelScarey)")
	resp, e := s.client.Do(req)
	if e != nil {
		return out, &Error{Code: "franchise_unavailable", Message: "Wikidata franchise discovery is unavailable. Try again later."}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out, &Error{Code: "franchise_unavailable", Message: "Wikidata franchise discovery is unavailable or rate limited. Try again later."}
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if e != nil || len(data) > 2<<20 {
		return out, &Error{Code: "franchise_invalid_response", Message: "Wikidata returned an unreadable franchise response."}
	}
	type value struct {
		Value string `json:"value"`
	}
	var body struct {
		Results struct {
			Bindings []struct {
				Seed           value `json:"seed"`
				Franchise      value `json:"franchise"`
				FranchiseLabel value `json:"franchiseLabel"`
				Member         value `json:"member"`
				MemberLabel    value `json:"memberLabel"`
				Movie          value `json:"movie"`
				TV             value `json:"tv"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if json.Unmarshal(data, &body) != nil || body.Results.Bindings == nil || len(body.Results.Bindings) > 201 {
		return out, &Error{Code: "franchise_invalid_response", Message: "Wikidata returned an unreadable franchise response."}
	}
	seed := ""
	seen := map[string]bool{}
	out.Truncated = len(body.Results.Bindings) > 200
	for _, v := range body.Results.Bindings[:min(len(body.Results.Bindings), 200)] {
		sid, fid, mid := wikidataID(v.Seed.Value), wikidataID(v.Franchise.Value), wikidataID(v.Member.Value)
		if sid == "" || fid == "" || mid == "" || (seed != "" && seed != sid) || !validLabel(v.MemberLabel.Value) || !validLabel(v.FranchiseLabel.Value) {
			return FranchiseResults{}, &Error{Code: "franchise_invalid_response", Message: "Wikidata returned an ambiguous or invalid franchise response."}
		}
		seed = sid
		typ, raw := "movie", v.Movie.Value
		if v.TV.Value != "" {
			if raw != "" {
				return FranchiseResults{}, &Error{Code: "franchise_invalid_response", Message: "Wikidata returned ambiguous title identifiers."}
			}
			typ, raw = "series", v.TV.Value
		}
		number, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || number < 1 || strconv.FormatInt(number, 10) != raw {
			return FranchiseResults{}, &Error{Code: "franchise_invalid_response", Message: "Wikidata returned an invalid title identifier."}
		}
		if typ == kind && number == id {
			continue
		}
		dedupe := fid + ":" + typ + ":" + raw
		if seen[dedupe] {
			continue
		}
		seen[dedupe] = true
		out.Items = append(out.Items, FranchiseMember{Kind: typ, TMDbID: number, Title: v.MemberLabel.Value, WikidataID: mid, FranchiseID: fid, Franchise: v.FranchiseLabel.Value})
	}
	sort.Slice(out.Items, func(i, j int) bool {
		if out.Items[i].Franchise != out.Items[j].Franchise {
			return out.Items[i].Franchise < out.Items[j].Franchise
		}
		return out.Items[i].Title < out.Items[j].Title
	})
	s.mu.Lock()
	if len(s.cache) >= 128 {
		s.cache = map[string]franchiseCache{}
	}
	s.cache[key] = franchiseCache{out, time.Now().Add(30 * time.Minute)}
	s.mu.Unlock()
	return out, nil
}
