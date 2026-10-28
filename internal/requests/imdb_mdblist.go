package requests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// MDBList is an optional, explicitly configured intermediary. The user imports
// their IMDb list there first; no IMDb access controls are bypassed here.
func (s *Lists) fetchIMDbMDBList(ctx context.Context, id, key string) ([]listTitle, error) {
	get := func(endpoint string, q url.Values) ([]byte, http.Header, error) {
		q.Set("apikey", key)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.mdblist.com"+endpoint+"?"+q.Encode(), nil)
		if err != nil {
			return nil, nil, ErrList
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "ReelScarey/0.20")
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, nil, errors.New("MDBList could not be reached.")
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
		resp.Body.Close()
		if err != nil || len(body) > 2<<20 || resp.StatusCode != http.StatusOK {
			return nil, nil, errors.New("MDBList could not provide the list. Check the list and API key.")
		}
		return body, resp.Header, nil
	}
	body, _, err := get("/external/lists/user", url.Values{})
	if err != nil {
		return nil, err
	}
	var catalogs []struct {
		ID     int64  `json:"id"`
		Source string `json:"source"`
	}
	if json.Unmarshal(body, &catalogs) != nil || len(catalogs) > 500 {
		return nil, errors.New("MDBList returned an unreadable list catalog.")
	}
	listID, _ := strconv.ParseInt(id, 10, 64)
	found := false
	for _, v := range catalogs {
		source := strings.ToLower(strings.TrimSpace(v.Source))
		if v.ID == listID && (strings.HasPrefix(source, "imdb") || strings.HasPrefix(source, "https://www.imdb.com/")) {
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("This MDBList account does not have that IMDb external list.")
	}
	items := []listTitle{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 10; page++ {
		q := url.Values{"limit": {"100"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		body, header, e := get("/external/lists/"+id+"/items", q)
		if e != nil {
			return nil, e
		}
		var data struct {
			NextCursor string `json:"next_cursor"`
			Movies     []struct {
				ID    int64  `json:"id"`
				Title string `json:"title"`
				IDs   struct {
					TMDb int64 `json:"tmdb"`
				} `json:"ids"`
			} `json:"movies"`
			Shows []struct {
				ID    int64  `json:"id"`
				Title string `json:"title"`
				IDs   struct {
					TMDb int64 `json:"tmdb"`
				} `json:"ids"`
			} `json:"shows"`
			Pagination struct {
				NextCursor string `json:"next_cursor"`
			} `json:"pagination"`
		}
		if json.Unmarshal(body, &data) != nil || len(data.Movies)+len(data.Shows) > 100 {
			return nil, errors.New("MDBList returned an unreadable IMDb list.")
		}
		for _, v := range data.Movies {
			tmdb := v.ID
			if v.IDs.TMDb > 0 {
				tmdb = v.IDs.TMDb
			}
			if tmdb < 1 || strings.TrimSpace(v.Title) == "" || len(v.Title) > 400 {
				return nil, errors.New("An IMDb list item is missing a TMDb identity.")
			}
			items = append(items, listTitle{"movie", tmdb, v.Title})
		}
		for _, v := range data.Shows {
			tmdb := v.ID
			if v.IDs.TMDb > 0 {
				tmdb = v.IDs.TMDb
			}
			if tmdb < 1 || strings.TrimSpace(v.Title) == "" || len(v.Title) > 400 {
				return nil, errors.New("An IMDb list item is missing a TMDb identity.")
			}
			items = append(items, listTitle{"series", tmdb, v.Title})
		}
		if len(items) > 200 {
			return nil, errors.New("A list can contain at most 200 movies and shows.")
		}
		more := strings.ToLower(strings.TrimSpace(header.Get("X-Has-More")))
		next := data.Pagination.NextCursor
		if next == "" {
			next = data.NextCursor
		}
		if more == "false" && next == "" {
			return items, nil
		}
		if more != "true" || next == "" || len(next) > 512 || seen[next] {
			return nil, errors.New("MDBList pagination could not be verified.")
		}
		seen[next] = true
		cursor = next
	}
	return nil, errors.New("The list exceeds the supported page limit.")
}
