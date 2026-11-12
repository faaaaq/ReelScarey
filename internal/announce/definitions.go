// Package announce captures tracker IRC announce lines and stores the parsed
// release facts for review. Capture-only is the default; explicitly opted-in
// supported sources also create durable, revision-fenced automatic-grab work.
package announce

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Definition describes one tracker's announce feed. Server, port and channel
// come from the tracker's published IRC setup; the regex follows the formats
// documented by the open-source Autobrr definitions.
type Definition struct {
	ID             string
	Name           string
	Server         string
	Port           int
	TLS            bool
	Channel        string
	RequireAccount bool
	BinarySizes    bool
	InfoPath       string
	KeyLabel       string
	Pattern        string
	re             *regexp.Regexp
}

// Parsed is one decoded announce line.
type Parsed struct {
	ReleaseName string
	Category    string
	SizeBytes   int64
	Freeleech   string
	TorrentID   string
	InfoURL     string
}

var (
	defOnce sync.Once
	defs    map[string]Definition
	defErr  error
)

// Definitions returns the compiled registry. A malformed definition is
// reported but does not prevent the valid ones from being used.
func Definitions() (map[string]Definition, error) {
	defOnce.Do(func() { defs, defErr = compileDefinitions() })
	return defs, defErr
}

func compileDefinitions() (map[string]Definition, error) {
	raw := []Definition{
		{
			ID: "iptorrents", Name: "IPTorrents", Server: "irc.iptorrents.com", Port: 6697, TLS: true,
			Channel: "#ipt.announce", BinarySizes: true, InfoPath: "/details.php?id={{torrentId}}", KeyLabel: "Passkey",
			Pattern: `^\[(?P<category>.+?)]\s+(?P<releaseName>.*?)\s*(?:(?P<freeleech>FREELEECH))?\s*-\s*(?P<baseUrl>https?://.+?/).+id=(?P<torrentId>\d+)\s*-\s*(?P<torrentSize>.*)$`,
		},
		{
			ID: "torrentleech", Name: "TorrentLeech", Server: "irc.torrentleech.org", Port: 7021, TLS: true,
			Channel: "#tlannounces", InfoPath: "/torrent/{{torrentId}}", KeyLabel: "RSS key",
			Pattern: `^New Torrent Announcement:\s*<(?P<category>[^>]*)>\s*Name:'(?P<releaseName>.*)' uploaded by '(?P<uploader>[^']*)'\s*(?:(?P<freeleech>freeleech))?\s*-\s*(?P<baseUrl>https?://[^/]+/)torrent/(?P<torrentId>\d+)`,
		},
		{
			ID: "filelist", Name: "FileList", Server: "irc.filelist.io", Port: 6697, TLS: true,
			Channel: "#announce", InfoPath: "/details.php?id={{torrentId}}", KeyLabel: "Passkey",
			Pattern: `^New Torrent:\s+(?P<releaseName>.*?)\s*(?:-- \[(?P<freeleech>FreeLeech)!]\s*)?(?:-- \[(?P<origin>Internal)!]\s*)?-- \[(?P<category>.*)]\s*\[(?P<torrentSize>.*)]\s*--\s*(?P<baseUrl>https?://filelist\.io/).*id=(?P<torrentId>\d+)\s*-- by (?P<uploader>.*)$`,
		},
		{
			ID: "beyondhd", Name: "BeyondHD", Server: "irc.beyond-hd.me", Port: 6697, TLS: true,
			Channel: "#bhd_announce", RequireAccount: true, InfoPath: "/torrents/autodl?id={{torrentId}}", KeyLabel: "RSS key",
			Pattern: `^New Torrent:\s+(?P<releaseName>.*?)\s*(?:\[(?P<freeleech>.*)]\s*)?Category:\s*(?P<category>.*?)\s+By:\s*(?P<uploader>.*?)\s+Size:\s*(?P<torrentSize>.*?)\s+Link:\s*(?P<baseUrl>https?://[^/]+/).*[&?]id=(?P<torrentId>\d+)`,
		},
		{
			ID: "animebytes", Name: "AnimeBytes", Server: "irc.animefriends.moe", Port: 7000, TLS: true,
			Channel: "#announce", InfoPath: "/torrents.php?id={{groupId}}&torrentid={{torrentId}}", KeyLabel: "Passkey",
			Pattern: `^(?P<releaseName>.*?)\s{2,}\[(?P<year>\d+)]\s::\s(?P<releaseTags>.*?)\s*\|\|\s*(?P<baseUrl>https?://[^ ]+/).*[?&]id=(?P<groupId>\d+)&torrentid=(?P<torrentId>\d+).*?(?:\|\|\s*(?P<tags>[^|]*))?(?:\|\|\s*Uploaded by:\s*(?P<uploader>.*))?$`,
		},
		{
			ID: "myanonamouse", Name: "MyAnonamouse", Server: "irc.myanonamouse.net", Port: 6697, TLS: true,
			Channel: "#announce", RequireAccount: true, InfoPath: "/t/{{torrentId}}", KeyLabel: "Session cookie (mam_id)",
			Pattern: `^(?P<releaseName>.*?By:\s*(?P<author>.+?)\s*\[(?P<language>[^\]]+)]\s*\[(?P<category>[^\]]+)]\s*\[(?P<subCategory>[^\]]+)]\s*\[(?P<container>[^\]]+)])\s*\[(?P<torrentSize>[^\]]+)]\s*-\s*(?P<tags>.+?)\s*-\s*(?P<baseUrl>https?://[^/]+/)t/(?P<torrentId>\d+)\s*(?P<releaseTags>VIP|Normal)?$`,
		},
	}
	out := map[string]Definition{}
	var problems []string
	for _, d := range raw {
		re, e := regexp.Compile(d.Pattern)
		if e != nil {
			problems = append(problems, d.Name+": "+e.Error())
			continue
		}
		d.re = re
		out[d.ID] = d
	}
	if len(problems) > 0 {
		return out, errors.New(strings.Join(problems, "; "))
	}
	return out, nil
}

// Parse matches one line and renders the tracker info URL. It reports false
// for unrelated channel chatter.
func (d Definition) Parse(line string) (Parsed, bool) {
	if d.re == nil {
		return Parsed{}, false
	}
	// Tracker bots commonly use mIRC colors, bold and reset markers. Match the
	// human-readable text, while callers retain the original announce for
	// evidence and deduplication.
	match := d.re.FindStringSubmatch(stripIRCFormatting(line))
	if match == nil {
		return Parsed{}, false
	}
	groups := map[string]string{}
	for i, name := range d.re.SubexpNames() {
		if i > 0 && name != "" {
			groups[name] = match[i]
		}
	}
	name := strings.TrimSpace(groups["releaseName"])
	if name == "" {
		return Parsed{}, false
	}
	path := render(d.InfoPath, groups)
	base := strings.TrimRight(strings.TrimSpace(groups["baseUrl"]), "/")
	info := ""
	if path != "" && base != "" {
		info = base + "/" + strings.TrimLeft(path, "/")
	}
	return Parsed{
		ReleaseName: name,
		Category:    strings.TrimSpace(groups["category"]),
		SizeBytes:   parseSize(groups["torrentSize"], d.BinarySizes),
		Freeleech:   firstNonEmpty(strings.TrimSpace(groups["freeleech"]), strings.TrimSpace(groups["freeleechEnum"])),
		TorrentID:   strings.TrimSpace(groups["torrentId"]),
		InfoURL:     info,
	}, true
}

func render(template string, groups map[string]string) string {
	out := template
	for key, value := range groups {
		out = strings.ReplaceAll(out, "{{"+key+"}}", value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

var sizePattern = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*([KMGT]i?B)$`)

// parseSize converts a displayed announce size. When binary is set (IPTorrents
// reports GB but means binary units), a plain GB is treated as GiB.
func parseSize(raw string, binary bool) int64 {
	m := sizePattern.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(raw)))
	if m == nil {
		return 0
	}
	value, e := strconv.ParseFloat(m[1], 64)
	if e != nil || value < 0 {
		return 0
	}
	unit := m[2]
	base := float64(1000)
	if strings.Contains(unit, "I") || binary {
		base = 1024
	}
	power := 0
	switch unit[0] {
	case 'K':
		power = 1
	case 'M':
		power = 2
	case 'G':
		power = 3
	case 'T':
		power = 4
	}
	size := value
	for i := 0; i < power; i++ {
		size *= base
	}
	if size < 0 || size > 1<<62 {
		return 0
	}
	return int64(size)
}

func (d Definition) String() string {
	return fmt.Sprintf("%s (%s:%d %s)", d.Name, d.Server, d.Port, d.Channel)
}
