package metadata

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const personCreditLimit = 24

type PersonCredit struct {
	Kind      string `json:"kind"`
	TMDbID    int64  `json:"tmdb_id"`
	Title     string `json:"title"`
	Year      *int   `json:"year,omitempty"`
	Date      string `json:"date,omitempty"`
	Character string `json:"character"`
	PosterURL string `json:"poster_url"`
	Category  string `json:"category"`
}

type personCreditsProvider interface {
	PersonCredits(context.Context, string, int64) ([]PersonCredit, error)
}

// PersonCredits returns a person's most recent cast roles across movies and TV.
// It is an explicit read for the cast card; no library identity changes.
func (s *Service) PersonCredits(ctx context.Context, id int64) ([]PersonCredit, error) {
	if id < 1 || id > 2147483647 {
		return nil, &Error{Code: "invalid_person_id", Message: "Choose a valid TMDb person."}
	}
	provider, ok := s.provider.(personCreditsProvider)
	if !ok {
		return nil, &Error{Code: "metadata_unavailable", Message: "Person credits are unavailable from this provider."}
	}
	token, err := s.token(ctx)
	if err != nil {
		return nil, err
	}
	return provider.PersonCredits(ctx, token, id)
}

func (c *Client) PersonCredits(ctx context.Context, token string, id int64) ([]PersonCredit, error) {
	if id < 1 || id > 2147483647 {
		return nil, &Error{Code: "invalid_person_id", Message: "Choose a valid TMDb person."}
	}
	var out struct {
		Cast []struct {
			ID               int64    `json:"id"`
			MediaType        string   `json:"media_type"`
			Title            string   `json:"title"`
			Name             string   `json:"name"`
			ReleaseDate      string   `json:"release_date"`
			FirstAirDate     string   `json:"first_air_date"`
			Character        string   `json:"character"`
			PosterPath       string   `json:"poster_path"`
			Popularity       float64  `json:"popularity"`
			VoteCount        int64    `json:"vote_count"`
			Adult            bool     `json:"adult"`
			GenreIDs         []int    `json:"genre_ids"`
			OriginalLanguage string   `json:"original_language"`
			OriginCountry    []string `json:"origin_country"`
		} `json:"cast"`
	}
	if err := c.request(ctx, token, "/person/"+strconv.FormatInt(id, 10)+"/combined_credits", url.Values{"language": {"en-US"}}, &out); err != nil {
		return nil, err
	}
	type entry struct {
		credit PersonCredit
		date   string
		pop    float64
		votes  int64
	}
	entries := []entry{}
	seen := map[string]bool{}
	for _, v := range out.Cast {
		kind := ""
		switch v.MediaType {
		case "movie":
			kind = "movie"
		case "tv":
			kind = "series"
		default:
			continue
		}
		title := strings.TrimSpace(v.Title)
		if kind == "series" {
			title = strings.TrimSpace(v.Name)
		}
		if v.ID < 1 || title == "" || utf8.RuneCountInString(title) > 500 || strings.IndexFunc(title, unicode.IsControl) >= 0 || v.Adult {
			continue
		}
		key := kind + ":" + strconv.FormatInt(v.ID, 10)
		if seen[key] {
			continue
		}
		seen[key] = true
		date := v.ReleaseDate
		if kind == "series" {
			date = v.FirstAirDate
		}
		if len(date) != 10 {
			date = ""
		} else if _, err := time.Parse("2006-01-02", date); err != nil {
			date = ""
		}
		var year *int
		if date != "" {
			if y, err := strconv.Atoi(date[:4]); err == nil {
				year = &y
			}
		}
		character := cleanCreditText(v.Character)
		category := "standard"
		if isAnime(v.GenreIDs, v.OriginalLanguage, v.OriginCountry) {
			category = "anime"
		}
		entries = append(entries, entry{
			credit: PersonCredit{Kind: kind, TMDbID: v.ID, Title: title, Year: year, Date: date, Character: character, PosterURL: PosterURL(v.PosterPath), Category: category},
			date:   date,
			pop:    v.Popularity,
			votes:  v.VoteCount,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].date != entries[j].date {
			if entries[i].date == "" {
				return false
			}
			if entries[j].date == "" {
				return true
			}
			return entries[i].date > entries[j].date
		}
		if entries[i].pop != entries[j].pop {
			return entries[i].pop > entries[j].pop
		}
		if entries[i].votes != entries[j].votes {
			return entries[i].votes > entries[j].votes
		}
		return strings.ToLower(entries[i].credit.Title) < strings.ToLower(entries[j].credit.Title)
	})
	outCredits := make([]PersonCredit, 0, personCreditLimit)
	for _, entry := range entries {
		if len(outCredits) == personCreditLimit {
			break
		}
		outCredits = append(outCredits, entry.credit)
	}
	return outCredits, nil
}

// isAnime uses TMDb's animation genre plus Japanese language or production
// country. Western animation remains a standard movie/show.
func isAnime(genres []int, language string, countries []string) bool {
	animated := false
	for _, genre := range genres {
		if genre == 16 {
			animated = true
			break
		}
	}
	if !animated {
		return false
	}
	if strings.EqualFold(language, "ja") {
		return true
	}
	for _, country := range countries {
		if strings.EqualFold(country, "JP") {
			return true
		}
	}
	return false
}

func cleanCreditText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Cast"
	}
	if utf8.RuneCountInString(value) > 300 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "Cast"
	}
	return value
}
