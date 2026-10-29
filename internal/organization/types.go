package organization

import (
	"context"
	"database/sql"
	"github.com/faaaaq/ReelScarey/internal/config"
	"os"
	"sync"
)

var mediaGate sync.RWMutex

// ReadMedia excludes a reviewed reorganization while normal media work uses paths.
// One application owns a configuration directory through the OS instance lock.
func ReadMedia() func() { mediaGate.RLock(); return mediaGate.RUnlock }

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string           { return e.Message }
func problem(code, message string) error { return &Error{code, message} }

type Library struct {
	Key             string `json:"key"`
	Name            string `json:"name"`
	Folder          string `json:"folder"`
	Content         string `json:"content"`
	Preset          string `json:"preset"`
	MovieTemplate   string `json:"movie_template"`
	SeriesTemplate  string `json:"series_template"`
	EpisodeTemplate string `json:"episode_template"`
}
type Config struct {
	Revision  int       `json:"revision"`
	MainRoot  string    `json:"main_root"`
	Libraries []Library `json:"libraries"`
}
type Assignment struct {
	MediaID          int64   `json:"media_id"`
	LibraryKey       string  `json:"library_key"`
	Revision         int     `json:"revision"`
	UseDefault       bool    `json:"use_default"`
	EffectiveLibrary Library `json:"effective_library"`
}
type Episode struct {
	ID       int64  `json:"id"`
	Season   int    `json:"season"`
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Absolute *int   `json:"absolute"`
}
type Content struct {
	ID               int64  `json:"id"`
	Kind             string `json:"kind"`
	Category         string `json:"category"`
	CategoryRevision int    `json:"category_revision"`
	Title            string `json:"title"`
	Year             *int   `json:"year"`
	TMDbID           int64  `json:"tmdb_id"`
}
type Snapshot struct {
	Library             Library `json:"library"`
	Content             Content `json:"content"`
	AssignmentRevision  int     `json:"assignment_revision"`
	ConfigRevision      int     `json:"config_revision"`
	LegacyEpisodeNaming string  `json:"legacy_episode_naming"`
}
type Companion struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Size        int64  `json:"size_bytes"`
	Mtime       int64  `json:"mtime"`
	SHA         string `json:"sha256"`
	Identity    string `json:"identity"`
}
type PlannedFile struct {
	EmbeddedTitle string      `json:"embedded_title,omitempty"`
	FileID        int64       `json:"media_file_id"`
	MediaID       int64       `json:"media_id"`
	Title         string      `json:"title"`
	Source        string      `json:"source"`
	Destination   string      `json:"destination"`
	Size          int64       `json:"size_bytes"`
	Mtime         int64       `json:"mtime"`
	SHA           string      `json:"sha256"`
	Identity      string      `json:"identity"`
	Snapshot      Snapshot    `json:"naming_snapshot"`
	Episodes      []Episode   `json:"episodes"`
	Companions    []Companion `json:"companions"`
	Unchanged     bool        `json:"unchanged"`
}
type Plan struct {
	RestoresPlan        string        `json:"restores_plan,omitempty"`
	ID                  string        `json:"id"`
	State               string        `json:"state"`
	Message             string        `json:"message"`
	ConfigRevision      int           `json:"config_revision"`
	TargetLibrary       string        `json:"target_library"`
	EntireMediaID       int64         `json:"entire_media_id,omitempty"`
	Files               []PlannedFile `json:"files"`
	Created             int64         `json:"created_at"`
	Updated             int64         `json:"updated_at"`
	Root                string        `json:"root"`
	Phase               string        `json:"phase"`
	FolderMove          *FolderMove   `json:"folder_move,omitempty"`
	Replacement         *Replacement  `json:"replacement,omitempty"`
	UpdateEmbeddedTitle bool          `json:"update_embedded_title,omitempty"`
	EmbeddedTitle       string        `json:"embedded_title,omitempty"`
}
type FolderMove struct {
	Source      string          `json:"source"`
	Destination string          `json:"destination"`
	Inventory   folderInventory `json:"inventory"`
	Copy        bool            `json:"copy,omitempty"`
	Stage       string          `json:"stage,omitempty"`
	StageID     string          `json:"stage_identity,omitempty"`
}
type Service struct {
	db       *sql.DB
	cfg      config.Config
	mu       sync.Mutex
	healthMu sync.Mutex
	link     func(*os.Root, string, string) error
	probe    func(context.Context, *os.File) (string, error)
	tag      func(context.Context, *os.File, *os.File, string, string) (string, error)
}

func New(db *sql.DB, cfg config.Config) *Service { return &Service{db: db, cfg: cfg} }

// SetProbe supplies the media verifier used by replacement workers.
func (s *Service) SetProbe(probe func(context.Context, *os.File) (string, error)) { s.probe = probe }

func (s *Service) SetTagger(tag func(context.Context, *os.File, *os.File, string, string) (string, error)) {
	s.tag = tag
}
