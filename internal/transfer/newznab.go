package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/url"
	"strings"
)

// Newznab shares the documented caps/search wire format with Torznab, but
// descriptors and download identities are validated independently of torrents.
type Newznab struct{ *Torznab }

func NewNewznab() *Newznab { return &Newznab{NewTorznab()} }
func (n *Newznab) Search(ctx context.Context, c IndexerConfig, query string) ([]Candidate, error) {
	c.Kind = "newznab"
	return n.Torznab.Search(ctx, c, query)
}
func (n *Newznab) Fetch(ctx context.Context, c IndexerConfig, v Candidate) (Payload, string, error) {
	u, e := url.Parse(v.URL)
	if e != nil || v.Magnet != "" {
		return Payload{}, "", problem("invalid_release", "Choose a valid NZB release from a fresh search.")
	}
	body, e := n.get(ctx, c, u, 4<<20)
	if e != nil {
		return Payload{}, "", e
	}
	identity, e := NZBIdentity(body)
	if e != nil {
		return Payload{}, "", e
	}
	return Payload{NZB: body}, identity, nil
}

// No paths or script metadata in an NZB authorize filesystem operations. This
// bounded descriptor only supplies NNTP article IDs to SABnzbd.
func NZBIdentity(body []byte) (string, error) {
	invalid := func() (string, error) {
		return "", problem("unsupported_nzb", "The release is not a supported, bounded NZB article descriptor.")
	}
	if len(body) == 0 || len(body) > 4<<20 {
		return invalid()
	}
	d := xml.NewDecoder(bytes.NewReader(body))
	var n struct {
		XMLName xml.Name
		Files   []struct {
			Subject  string   `xml:"subject,attr"`
			Groups   []string `xml:"groups>group"`
			Segments []struct {
				Number int    `xml:"number,attr"`
				Bytes  int64  `xml:"bytes,attr"`
				ID     string `xml:",chardata"`
			} `xml:"segments>segment"`
		} `xml:"file"`
	}
	if d.Decode(&n) != nil || n.XMLName.Local != "nzb" || (n.XMLName.Space != "" && n.XMLName.Space != "http://www.newzbin.com/DTD/2003/nzb") || len(n.Files) == 0 || len(n.Files) > 5000 {
		return invalid()
	}
	for {
		tok, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return invalid()
		}
		if b, ok := tok.(xml.CharData); !ok || strings.TrimSpace(string(b)) != "" {
			return invalid()
		}
	}
	count := 0
	var total int64
	for _, f := range n.Files {
		if !textOK(f.Subject, 2000) || len(f.Groups) == 0 || len(f.Groups) > 100 || len(f.Segments) == 0 {
			return invalid()
		}
		for _, g := range f.Groups {
			if !textOK(strings.TrimSpace(g), 200) {
				return invalid()
			}
		}
		seen := map[int]bool{}
		for _, s := range f.Segments {
			count++
			total += s.Bytes
			if count > 100000 || s.Number < 1 || seen[s.Number] || s.Bytes <= 0 || s.Bytes > 1<<30 || total > 2<<40 || !textOK(strings.TrimSpace(s.ID), 1000) || strings.ContainsAny(s.ID, "<> \t") {
				return invalid()
			}
			seen[s.Number] = true
		}
	}
	h := sha256.Sum256(body)
	return "nzb:" + hex.EncodeToString(h[:]), nil
}
