package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

func (s *Server) watchProviders(w http.ResponseWriter, r *http.Request) {
	if s.metadata == nil {
		fail(w, 503, "metadata_unavailable", "TMDb metadata is not configured.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "invalid_movie", "Choose a valid TMDb movie ID.")
		return
	}
	region := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("region")))
	if region == "" {
		region = "US"
	}
	if len(region) != 2 || region[0] < 'A' || region[0] > 'Z' || region[1] < 'A' || region[1] > 'Z' {
		fail(w, 400, "invalid_region", "Use a two-letter ISO 3166-1 region such as US or GB.")
		return
	}
	out, err := s.metadata.WatchProviders(r.Context(), id, region)
	if errors.Is(err, metadata.ErrUnsupported) {
		fail(w, 501, "not_supported", err.Error())
		return
	}
	if err != nil {
		s.providerError(w, err)
		return
	}
	send(w, 200, map[string]any{"region": region, "providers": out})
}
