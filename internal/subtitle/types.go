// Package subtitle manages sidecars for already registered media, independently
// of indexers. Providers never receive local paths and never write library files.
package subtitle

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
)

const MaxSubtitle = 4 << 20

var ErrLease = errors.New("subtitle job lease or preferences changed")

type Error struct {
	Code, Message string
	Temporary     bool
	RetryAt       int64
}

func (e *Error) Error() string           { return e.Message }
func problem(code, message string) error { return &Error{Code: code, Message: message} }

type Track struct {
	Language string `json:"language"`
	Variant  string `json:"variant"`
}
type Language struct {
	Code    string   `json:"code"`
	Name    string   `json:"name"`
	Aliases []string `json:"-"`
}

var Languages = []Language{{"en", "English", []string{"eng", "english"}}, {"es", "Spanish", []string{"spa", "spanish"}}, {"fr", "French", []string{"fra", "fre", "french"}}, {"de", "German", []string{"deu", "ger", "german"}}, {"it", "Italian", []string{"ita", "italian"}}, {"ja", "Japanese", []string{"jpn", "japanese"}}, {"ko", "Korean", []string{"kor", "korean"}}, {"pt-br", "Portuguese (Brazil)", []string{"pob", "por-br"}}, {"pt-pt", "Portuguese", []string{"por", "pt"}}, {"zh-cn", "Chinese (simplified)", []string{"chi", "zho", "zh"}}, {"zh-tw", "Chinese (traditional)", []string{"cht"}}, {"nl", "Dutch", []string{"nld", "dut"}}, {"ru", "Russian", []string{"rus"}}, {"ar", "Arabic", []string{"ara"}}, {"hi", "Hindi", []string{"hin"}}, {"pl", "Polish", []string{"pol"}}, {"sv", "Swedish", []string{"swe"}}, {"uk", "Ukrainian", []string{"ukr"}}}

func language(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	for _, l := range Languages {
		if l.Code == code || slices.Contains(l.Aliases, code) {
			return l.Code
		}
	}
	return ""
}
func validTrack(t Track) bool {
	return language(t.Language) == t.Language && t.Language != "" && slices.Contains([]string{"full", "signs", "forced"}, t.Variant)
}
func textOK(v string, max int) bool {
	return v != "" && len(v) <= max && !strings.ContainsFunc(v, unicode.IsControl)
}

type Credentials struct {
	APIKey     string `json:"api_key"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	UserAgent  string `json:"user_agent"`
	Generation string `json:"generation"`
}
type CredentialUpdate struct {
	APIKey    *string `json:"api_key"`
	Username  string  `json:"username"`
	Password  *string `json:"password"`
	UserAgent string  `json:"user_agent"`
}
type Target struct {
	MediaID   int64
	Kind      string
	TMDbID    int64
	Title     string
	Year      *int
	Release   string
	Episodes  []Episode
	MovieHash string
	Size      int64
}
type Episode struct {
	TMDbID         int64
	Season, Number int
}
type Candidate struct {
	ID         string `json:"id"`
	FileID     int64  `json:"file_id"`
	SubtitleID string `json:"subtitle_id"`
	Track
	Release         string   `json:"release"`
	FileName        string   `json:"file_name"`
	HearingImpaired bool     `json:"hearing_impaired"`
	HashMatch       bool     `json:"hash_match"`
	Trusted         bool     `json:"trusted"`
	Downloads       int      `json:"downloads"`
	Automatic       bool     `json:"automatic"`
	CanDownload     bool     `json:"can_download"`
	Score           int      `json:"score"`
	Reasons         []string `json:"reasons"`
	Identity        bool     `json:"-"`
	Translated      bool     `json:"-"`
	Multipart       bool     `json:"-"`
}
type Download struct {
	Content   []byte
	Remaining *int
	ResetAt   int64
}
type Provider interface {
	Test(context.Context, Credentials) error
	Search(context.Context, Credentials, Target, []string) ([]Candidate, error)
	Fetch(context.Context, Credentials, Candidate) (Download, error)
}

func retry(message string, delay time.Duration) error {
	return &Error{Code: "provider_unavailable", Message: message, Temporary: true, RetryAt: time.Now().Add(delay).Unix()}
}
