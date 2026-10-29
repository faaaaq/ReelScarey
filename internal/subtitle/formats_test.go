package subtitle

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validASS = "[Script Info]\nScriptType: v4.00+\nTitle: Owned fixture\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize\nStyle: Default,Arial,24\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,{\\an8}{\\i1}Styled, text{\\i0}\\NSecond line\n"
const validVTT = "WEBVTT\n\nNOTE owned --> retain this comment\n\nSTYLE\n::cue { color: lime; }\n\nowned-cue\n00:00:01.000 --> 00:00:03.000 align:start\n<i>Owned text</i>\n"
const validSSA = "[Script Info]\nScriptType: v4.00\nTitle: Owned SSA fixture\n[V4 Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, TertiaryColour, BackColour, Bold, Italic, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, AlphaLevel, Encoding\nStyle: Default,Arial,24,16777215,65535,65535,-2147483640,-1,0,1,1,0,2,10,10,10,0,1\n[Events]\nFormat: Marked, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: Marked=0,0:00:01.00,0:00:03.00,Default,,0,0,0,,{\\an8}{\\i1}Styled, text{\\i0}\n"

func TestProviderFormatsPreserveStylesAndEnforceTiming(t *testing.T) {
	for _, format := range []string{"ass", "ssa", "vtt"} {
		body := validASS
		if format == "ssa" {
			body = validSSA
		}
		if format == "vtt" {
			body = validVTT
		}
		got, e := normalizeProviderSubtitle([]byte(body), format, 90)
		if e != nil || string(got) != body {
			t.Fatal(format, "valid payload changed", e)
		}
		if _, e = normalizeProviderSubtitle([]byte(strings.ReplaceAll(body, "00:03", "59:59")), format, 90); e == nil {
			t.Fatal("duration overrun accepted", format)
		}
		if _, e = normalizeProviderSubtitle([]byte(validSRT), format, 90); e == nil {
			t.Fatal("mislabeled SRT accepted", format)
		}
	}
	for _, body := range []string{
		strings.ReplaceAll(validASS, "End, Style", "Start, Style"),
		strings.ReplaceAll(validASS, "0:00:03.00", "0:00:00.00"),
		strings.ReplaceAll(validASS, "0:00:03.00", "0:00:NaN"),
		strings.ReplaceAll(validASS, "0:00:03.00", "0:00:3e1"),
		strings.ReplaceAll(validASS, "0:00:03.00", "0:00:60.00"),
		strings.ReplaceAll(validASS, "[Events]", "[Other]"),
		strings.ReplaceAll(validASS, "Text\nDialogue", "Text,Extra\nDialogue"),
		"<html>" + validASS,
		validASS + "\x00",
	} {
		if _, e := normalizeProviderSubtitle([]byte(body), "ass", 90); e == nil {
			t.Fatal("unsafe styled payload accepted")
		}
	}
	if _, e := normalizeProviderSubtitle([]byte(strings.ReplaceAll(validVTT, ".000", ",000")), "vtt", 90); e == nil {
		t.Fatal("SRT timestamps accepted as VTT")
	}
}
func TestOriginalFormatRankingAndProviderRequestAvoidConversion(t *testing.T) {
	p := Preferences{DownloadFormat: "original", Tracks: []Track{{"en", "full"}}, MatchPolicy: "hash"}
	in := Candidate{FileID: 45, Track: p.Tracks[0], Identity: true, HashMatch: true, FileName: "owned.ass"}
	ranked := rank(p, Target{}, []Candidate{in})
	if len(ranked) != 1 || !ranked[0].Automatic || ranked[0].Format != "ass" {
		t.Fatal(ranked)
	}
	in.FileName = "archive.zip"
	if got := rank(p, Target{}, []Candidate{in}); got[0].CanDownload || got[0].Format != "" {
		t.Fatal("unsupported original allowed", got)
	}
	calls := 0
	provider := protocol(t, func(r *http.Request) *http.Response {
		if r.URL.Host == "dl.opensubtitles.com" {
			if r.Header.Get("Api-Key") != "" || r.Header.Get("Authorization") != "" {
				t.Fatal("credentials leaked")
			}
			return response(200, validASS)
		}
		if r.URL.Path == "/api/v1/login" {
			return response(200, `{"token":"fixture-token","base_url":"api.opensubtitles.com"}`)
		}
		if r.URL.Path != "/api/v1/download" {
			t.Fatal(r.URL)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if len(body) != 1 || body["file_id"] != float64(45) {
			t.Fatal("original format was converted", body)
		}
		calls++
		return response(200, `{"link":"https://dl.opensubtitles.com/download/owned","remaining":3}`)
	})
	got, e := provider.Fetch(context.Background(), testCredentials, ranked[0])
	if e != nil || !bytes.Equal(got.Content, []byte(validASS)) || calls != 1 {
		t.Fatal(got, e)
	}
}

func TestStyledProviderImportsAndRecoveriesKeepFormatAndQuota(t *testing.T) {
	for _, format := range []string{"ass", "ssa", "vtt"} {
		t.Run(format, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			enable(t, f, true, Track{"en", "full"})
			p, _ := f.s.Preferences(ctx)
			p.DownloadFormat = "original"
			if _, e := f.s.SavePreferences(ctx, p); e != nil {
				t.Fatal(e)
			}
			f.p.matches = f.p.matches[:1]
			f.p.matches[0].FileName = "owned." + format
			body := validASS
			if format == "ssa" {
				body = validSSA
			}
			if format == "vtt" {
				body = validVTT
			}
			f.p.content = []byte(body)
			if e := f.s.Search(ctx, 1); e != nil {
				t.Fatal(e)
			}
			process(t, f.s)
			view, e := f.s.File(ctx, 1, true)
			if e != nil || len(view.Candidates) != 1 || view.Candidates[0].Format != format {
				t.Fatal(view, e)
			}
			candidate := view.Candidates[0]
			if e = f.s.Select(ctx, candidate.ID, true); e != nil {
				t.Fatal(e)
			}
			// Pre-journal the provider bytes, as if interrupted immediately after quota
			// consumption. Reopening must publish those bytes without another fetch.
			media, e := f.s.media(ctx, 1)
			if e != nil {
				t.Fatal(e)
			}
			plan, e := f.s.plan(ctx, media, candidate, []byte(body))
			if e != nil {
				t.Fatal(e)
			}
			if format != "ass" {
				stage := filepath.Join(filepath.Dir(f.video), plan.Stage)
				if e = os.WriteFile(stage, []byte(body), 0600); e != nil {
					t.Fatal(e)
				}
				if format == "vtt" {
					if e = os.Link(stage, filepath.Join(f.cfg.MediaDir, filepath.FromSlash(plan.Destination))); e != nil {
						t.Fatal(e)
					}
					if _, e = f.db.Exec(`UPDATE subtitle_imports SET phase='published' WHERE id=?`, plan.ID); e != nil {
						t.Fatal(e)
					}
					if e = os.Remove(stage); e != nil {
						t.Fatal(e)
					}
				}
			}
			prefs, _ := f.s.Preferences(ctx)
			prefs.DownloadFormat = "srt"
			if _, e = f.s.SavePreferences(ctx, prefs); e != nil {
				t.Fatal(e)
			}
			reopen(t, &f)
			process(t, f.s)
			if f.p.downloads != 0 {
				t.Fatal("recovery consumed quota again")
			}
			path := strings.TrimSuffix(f.video, ".mkv") + ".en." + format
			actual, e := os.ReadFile(path)
			if e != nil || string(actual) != body {
				t.Fatal("styled source changed", e)
			}
			var phase, dest string
			if e = f.db.QueryRow(`SELECT phase,destination FROM subtitle_imports WHERE id=?`, plan.ID).Scan(&phase, &dest); e != nil || phase != "committed" || !strings.HasSuffix(dest, ".en."+format) {
				t.Fatal(phase, dest, e)
			}
			edit, e := f.s.ReviewEdit(ctx, 1, EditRequest{Track: Track{"en", "full"}, Action: "offset", Offset: 1000})
			if e != nil {
				t.Fatal(e)
			}
			if e = f.s.QueueEdit(ctx, edit.ID, true); e != nil {
				t.Fatal(e)
			}
			if _, e = f.s.ProcessEdit(ctx); e != nil {
				t.Fatal(e)
			}
			shifted, e := os.ReadFile(path)
			if e != nil || (format != "vtt" && !strings.Contains(string(shifted), "{\\an8}{\\i1}Styled, text")) {
				t.Fatal("edit lost styling", e)
			}
			rollback, e := f.s.ReviewEdit(ctx, 1, EditRequest{Track: Track{"en", "full"}, Action: "rollback", PreviousID: edit.ID})
			if e != nil {
				t.Fatal(e)
			}
			if e = f.s.QueueEdit(ctx, rollback.ID, true); e != nil {
				t.Fatal(e)
			}
			if _, e = f.s.ProcessEdit(ctx); e != nil {
				t.Fatal(e)
			}
			restored, e := os.ReadFile(path)
			if e != nil || string(restored) != body {
				t.Fatal("styled rollback changed original", e)
			}
			video, e := os.ReadFile(f.video)
			if e != nil || !bytes.Equal(video, f.body) {
				t.Fatal("video changed", e)
			}
		})
	}
}

func TestFormatChangeDuringFetchCannotPublishStaleSelection(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	enable(t, f, true, Track{"en", "full"})
	p, _ := f.s.Preferences(ctx)
	p.DownloadFormat = "original"
	if _, e := f.s.SavePreferences(ctx, p); e != nil {
		t.Fatal(e)
	}
	f.p.matches = f.p.matches[:1]
	f.p.matches[0].FileName = "owned.ass"
	f.p.content = []byte(validASS)
	f.p.onFetch = func() {
		next, e := f.s.Preferences(ctx)
		if e != nil {
			t.Fatal(e)
		}
		next.DownloadFormat = "srt"
		if _, e = f.s.SavePreferences(ctx, next); e != nil {
			t.Fatal(e)
		}
	}
	process(t, f.s)
	var journals int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM subtitle_imports`).Scan(&journals); e != nil || journals != 0 || f.p.downloads != 1 {
		t.Fatal("stale format was journaled", journals, e)
	}
	if _, e := os.Stat(strings.TrimSuffix(f.video, ".mkv") + ".en.ass"); !os.IsNotExist(e) {
		t.Fatal("stale format was published", e)
	}
}
