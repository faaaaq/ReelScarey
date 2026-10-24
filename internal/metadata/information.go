package metadata

import (
	"context"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Person struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Image string `json:"image_url"`
}
type Information struct {
	Tagline    string   `json:"tagline"`
	Genres     []string `json:"genres"`
	Rating     float64  `json:"rating"`
	Votes      int64    `json:"votes"`
	IMDbURL    string   `json:"imdb_url"`
	TMDbURL    string   `json:"tmdb_url"`
	TrailerURL string   `json:"trailer_url"`
	Cast       []Person `json:"cast"`
	Crew       []Person `json:"crew"`
}
type InformationProvider interface {
	Information(context.Context, string, string, int64) (Information, error)
}

func (s *Service) Information(ctx context.Context, kind string, id int64) (Information, error) {
	p, ok := s.provider.(InformationProvider)
	if !ok {
		return Information{}, &Error{Code: "metadata_unavailable", Message: "Extended metadata is unavailable from this provider."}
	}
	token, e := s.token(ctx)
	if e != nil {
		return Information{}, e
	}
	return p.Information(ctx, token, kind, id)
}

type creditPerson struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Character string `json:"character"`
	Job       string `json:"job"`
	Profile   string `json:"profile_path"`
	Order     int    `json:"order"`
	Roles     []struct {
		Character string `json:"character"`
	} `json:"roles"`
	Jobs []struct {
		Job string `json:"job"`
	} `json:"jobs"`
}

func (c *Client) Information(ctx context.Context, token, kind string, id int64) (Information, error) {
	namespace := "movie"
	credits := "credits"
	if kind == "series" {
		namespace = "tv"
		credits = "aggregate_credits"
	} else if kind != "movie" {
		return Information{}, tvError()
	}
	var in struct {
		ID      int64   `json:"id"`
		Tagline string  `json:"tagline"`
		Rating  float64 `json:"vote_average"`
		Votes   int64   `json:"vote_count"`
		Genres  []struct {
			Name string `json:"name"`
		} `json:"genres"`
		External struct {
			IMDb string `json:"imdb_id"`
		} `json:"external_ids"`
		Creators []creditPerson `json:"created_by"`
		Credits  struct {
			Cast []creditPerson `json:"cast"`
			Crew []creditPerson `json:"crew"`
		} `json:"credits"`
		Aggregate struct {
			Cast []creditPerson `json:"cast"`
			Crew []creditPerson `json:"crew"`
		} `json:"aggregate_credits"`
		Videos struct {
			Results []struct {
				Key      string `json:"key"`
				Site     string `json:"site"`
				Type     string `json:"type"`
				Official bool   `json:"official"`
			}
		} `json:"videos"`
	}
	if e := c.requestLimit(ctx, token, "/"+namespace+"/"+strconv.FormatInt(id, 10), url.Values{"language": {"en-US"}, "append_to_response": {credits + ",external_ids,videos"}}, &in, 6<<20); e != nil {
		return Information{}, e
	}
	if in.ID != id {
		return Information{}, tvError()
	}
	out := Information{Genres: []string{}, Cast: []Person{}, Crew: []Person{}, TMDbURL: "https://www.themoviedb.org/" + namespace + "/" + strconv.FormatInt(id, 10)}
	if len(in.Tagline) <= 1000 {
		out.Tagline = in.Tagline
	}
	if in.Rating >= 0 && in.Rating <= 10 {
		out.Rating = in.Rating
	}
	out.Votes = max(0, in.Votes)
	if regexp.MustCompile(`^tt[0-9]{5,12}$`).MatchString(in.External.IMDb) {
		out.IMDbURL = "https://www.imdb.com/title/" + in.External.IMDb + "/"
	}
	for _, g := range in.Genres {
		if len(out.Genres) < 20 && len(g.Name) > 0 && len(g.Name) <= 80 {
			out.Genres = append(out.Genres, g.Name)
		}
	}
	cast, crew := in.Credits.Cast, in.Credits.Crew
	if kind == "series" {
		cast, crew = in.Aggregate.Cast, in.Aggregate.Crew
	}
	sort.SliceStable(cast, func(i, j int) bool { return cast[i].Order < cast[j].Order })
	clean := func(p creditPerson, role string) (Person, bool) {
		if p.ID < 1 || strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 || len(role) > 300 {
			return Person{}, false
		}
		return Person{p.ID, p.Name, role, PosterURL(p.Profile)}, true
	}
	seen := map[int64]bool{}
	for _, p := range cast {
		if len(out.Cast) == 12 {
			break
		}
		role := p.Character
		if role == "" && len(p.Roles) > 0 {
			role = p.Roles[0].Character
		}
		if v, ok := clean(p, role); ok && !seen[v.ID] {
			out.Cast = append(out.Cast, v)
			seen[v.ID] = true
		}
	}
	crewSeen := map[string]bool{}
	addCrew := func(p creditPerson, role string) {
		if len(out.Crew) == 30 {
			return
		}
		key := strconv.FormatInt(p.ID, 10) + role
		if v, ok := clean(p, role); ok && !crewSeen[key] {
			out.Crew = append(out.Crew, v)
			crewSeen[key] = true
		}
	}
	for _, p := range in.Creators {
		addCrew(p, "Creator")
	}
	for _, p := range crew {
		jobs := []string{p.Job}
		for _, j := range p.Jobs {
			jobs = append(jobs, j.Job)
		}
		for _, job := range jobs {
			switch job {
			case "Director", "Writer", "Screenplay", "Story", "Novel", "Teleplay", "Original Series Creator":
				addCrew(p, job)
			}
		}
	}
	for _, v := range in.Videos.Results {
		if v.Site == "YouTube" && v.Type == "Trailer" && regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`).MatchString(v.Key) {
			if out.TrailerURL == "" || v.Official {
				out.TrailerURL = "https://www.youtube.com/watch?v=" + v.Key
			}
			if v.Official {
				break
			}
		}
	}
	return out, nil
}
