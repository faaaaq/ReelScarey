package nfo

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrNotEpisode reports an XML document that is not an episode NFO. Cleanup
// never guesses at a document's type or rewrites a movie/series sidecar.
var ErrNotEpisode = errors.New("NFO is not an episode details document")

const blankSortTitle = "<sorttitle></sorttitle>"

type sortTitleSpan struct {
	start int64
	end   int64
}

// ClearEpisodeSortTitle blanks every non-empty sorttitle directly below the
// episodedetails root. Other elements, comments and formatting are retained
// byte-for-byte. Missing and already-empty sort titles need no write.
//
// The input is parsed as XML rather than searched as text, so sorttitle-looking
// text in plots, comments or nested metadata is never changed. Invalid XML and
// non-episode documents return an error and leave the input untouched.
func ClearEpisodeSortTitle(document []byte) ([]byte, bool, error) {
	decoder := xml.NewDecoder(bytes.NewReader(document))
	depth := 0
	rootSeen := false
	rootClosed := false
	clearingDepth := -1
	clearingStart := int64(0)
	clearingHasValue := false
	spans := []sortTitleSpan{}

	for {
		start := decoder.InputOffset()
		token, err := decoder.Token()
		end := decoder.InputOffset()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return document, false, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if rootSeen || value.Name.Local != "episodedetails" {
					return document, false, ErrNotEpisode
				}
				rootSeen = true
			}
			if clearingDepth >= 0 {
				// Nested markup is not a meaningful blank sort title. Blank the
				// complete direct-child element while parsing through it safely.
				clearingHasValue = true
			} else if depth == 1 && value.Name.Local == "sorttitle" {
				clearingDepth = depth
				clearingStart = start
				clearingHasValue = false
			}
			depth++
		case xml.CharData:
			if clearingDepth >= 0 && strings.TrimSpace(string(value)) != "" {
				clearingHasValue = true
			}
		case xml.EndElement:
			if depth == 0 {
				return document, false, fmt.Errorf("%w: unexpected closing element", ErrInvalid)
			}
			depth--
			if clearingDepth == depth {
				if clearingHasValue {
					spans = append(spans, sortTitleSpan{start: clearingStart, end: end})
				}
				clearingDepth = -1
			}
			if depth == 0 {
				rootClosed = true
			}
		}
	}
	if !rootSeen || !rootClosed || depth != 0 {
		return document, false, fmt.Errorf("%w: incomplete episode details", ErrInvalid)
	}
	if len(spans) == 0 {
		return document, false, nil
	}

	var out bytes.Buffer
	out.Grow(len(document))
	position := int64(0)
	for _, span := range spans {
		if span.start < position || span.end < span.start || span.end > int64(len(document)) {
			return document, false, fmt.Errorf("%w: invalid sort title offsets", ErrInvalid)
		}
		out.Write(document[position:span.start])
		out.WriteString(blankSortTitle)
		position = span.end
	}
	out.Write(document[position:])
	return out.Bytes(), true, nil
}
