package metadata

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// WatchOffer is one flat-rate, rental, purchase or free provider. LogoURL is
// empty when TMDb returned no usable image path.
type WatchOffer struct {
	Name    string `json:"name"`
	LogoURL string `json:"logo_url"`
}

// WatchProviders is region-scoped availability from TMDb. It is a suggestion,
// not a promise that playback is licensed or currently working.
type WatchProviders struct {
	Link     string       `json:"link"`
	Flatrate []WatchOffer `json:"flatrate"`
	Rent     []WatchOffer `json:"rent"`
	Buy      []WatchOffer `json:"buy"`
	Free     []WatchOffer `json:"free"`
}

type WatchProvider interface {
	WatchProviders(context.Context, string, int64, string) (WatchProviders, error)
}

var ErrUnsupported = errors.New("This metadata provider does not expose watch availability.")

func (s *Service) WatchProviders(ctx context.Context, id int64, region string) (WatchProviders, error) {
	provider, ok := s.provider.(WatchProvider)
	if !ok {
		return WatchProviders{}, ErrUnsupported
	}
	token, err := s.token(ctx)
	if err != nil {
		return WatchProviders{}, err
	}
	return provider.WatchProviders(ctx, token, id, region)
}

func (c *Client) WatchProviders(ctx context.Context, token string, id int64, region string) (WatchProviders, error) {
	var body struct {
		Results map[string]struct {
			Link     string `json:"link"`
			Flatrate []struct {
				Name     string `json:"provider_name"`
				LogoPath string `json:"logo_path"`
			} `json:"flatrate"`
			Rent []struct {
				Name     string `json:"provider_name"`
				LogoPath string `json:"logo_path"`
			} `json:"rent"`
			Buy []struct {
				Name     string `json:"provider_name"`
				LogoPath string `json:"logo_path"`
			} `json:"buy"`
			Free []struct {
				Name     string `json:"provider_name"`
				LogoPath string `json:"logo_path"`
			} `json:"free"`
		} `json:"results"`
	}
	if err := c.request(ctx, token, "/movie/"+strconv.FormatInt(id, 10)+"/watch/providers", url.Values{"language": {"en-US"}}, &body); err != nil {
		return WatchProviders{}, err
	}
	entry, ok := body.Results[region]
	if !ok {
		return WatchProviders{}, nil
	}
	return WatchProviders{
		Link:     safeProviderLink(entry.Link),
		Flatrate: offers(entry.Flatrate),
		Rent:     offers(entry.Rent),
		Buy:      offers(entry.Buy),
		Free:     offers(entry.Free),
	}, nil
}

func offers(in []struct {
	Name     string `json:"provider_name"`
	LogoPath string `json:"logo_path"`
}) []WatchOffer {
	out := []WatchOffer{}
	for _, item := range in {
		name := strings.TrimSpace(item.Name)
		if name == "" || len(name) > 120 {
			continue
		}
		logo := ""
		if posterPattern.MatchString(item.LogoPath) {
			logo = "https://image.tmdb.org/t/p/w92" + item.LogoPath
		}
		out = append(out, WatchOffer{Name: name, LogoURL: logo})
	}
	return out
}

func safeProviderLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || (u.Hostname() != "www.themoviedb.org" && u.Hostname() != "themoviedb.org") {
		return ""
	}
	return raw
}
