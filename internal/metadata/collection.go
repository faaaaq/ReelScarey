package metadata

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

type Collection struct {
	Name  string  `json:"name"`
	Items []Movie `json:"items"`
}
type collectionProvider interface {
	MovieCollection(context.Context, string, int64) (Collection, error)
}

func (s *Service) MovieCollection(ctx context.Context, id int64) (Collection, error) {
	p, ok := s.provider.(collectionProvider)
	if !ok {
		return Collection{}, &Error{Code: "metadata_unavailable", Message: "Movie collections are unavailable from this provider."}
	}
	token, e := s.token(ctx)
	if e != nil {
		return Collection{}, e
	}
	return p.MovieCollection(ctx, token, id)
}
func (c *Client) MovieCollection(ctx context.Context, token string, id int64) (Collection, error) {
	out := Collection{Items: []Movie{}}
	if id < 1 {
		return out, &Error{Code: "invalid_movie", Message: "Choose a valid movie."}
	}
	var detail struct {
		ID         int64 `json:"id"`
		Collection *struct {
			ID int64 `json:"id"`
		} `json:"belongs_to_collection"`
	}
	if e := c.request(ctx, token, "/movie/"+strconv.FormatInt(id, 10), url.Values{"language": {"en-US"}}, &detail); e != nil {
		return out, e
	}
	if detail.ID != id {
		return out, &Error{Code: "tmdb_invalid_response", Message: "TMDb returned a different movie."}
	}
	if detail.Collection == nil {
		return out, nil
	}
	if detail.Collection.ID < 1 {
		return out, &Error{Code: "tmdb_invalid_response", Message: "TMDb returned an invalid collection."}
	}
	var collection struct {
		ID    int64           `json:"id"`
		Name  string          `json:"name"`
		Parts []movieResponse `json:"parts"`
	}
	if e := c.request(ctx, token, "/collection/"+strconv.FormatInt(detail.Collection.ID, 10), url.Values{"language": {"en-US"}}, &collection); e != nil {
		return out, e
	}
	if collection.ID != detail.Collection.ID || strings.TrimSpace(collection.Name) == "" || len(collection.Name) > 500 || len(collection.Parts) > 500 {
		return out, &Error{Code: "tmdb_invalid_response", Message: "TMDb returned an invalid collection."}
	}
	out.Name = collection.Name
	for _, part := range collection.Parts {
		movie, e := normalize(part)
		if e != nil {
			return Collection{}, e
		}
		out.Items = append(out.Items, movie)
	}
	return out, nil
}
