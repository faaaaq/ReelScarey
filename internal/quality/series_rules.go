package quality

import (
	"regexp"
	"strings"
)

var releasePrefix = regexp.MustCompile(`^\[([^\[\]]{1,80})\]`)
var releaseSuffix = regexp.MustCompile(`-([A-Za-z0-9][A-Za-z0-9._]{0,79})$`)

func matchesReleaseGroup(title, group string) bool {
	want := Normalize(group)
	if want == "" {
		return false
	}
	title = strings.TrimSpace(title)
	if m := releasePrefix.FindStringSubmatch(title); len(m) > 1 && Normalize(m[1]) == want {
		return true
	}
	if m := releaseSuffix.FindStringSubmatch(title); len(m) > 1 && Normalize(m[1]) == want {
		return true
	}
	return false
}
