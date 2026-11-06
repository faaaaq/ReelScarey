package transfer

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func bstr(s string) string { return strconv.Itoa(len(s)) + ":" + s }

func torrentInfoDict(body string) string {
	return "d" +
		bstr("announce") + bstr("http://tracker/announce") +
		bstr("comment") + bstr("fixture") +
		bstr("info") + body +
		"e"
}

func singleFileTorrent(name string, length int64) ([]byte, string) {
	info := "d" +
		bstr("length") + "i" + strconv.FormatInt(length, 10) + "e" +
		bstr("name") + bstr(name) +
		bstr("piece length") + "i16384e" +
		bstr("pieces") + bstr(strings.Repeat("P", 20)) + "e"
	sum := sha1.Sum([]byte(info))
	return []byte(torrentInfoDict(info)), hex.EncodeToString(sum[:])
}

func multiFileTorrent(name string, lengths ...int64) ([]byte, string) {
	entries := ""
	for i, length := range lengths {
		entries += "d" + bstr("length") + "i" + strconv.FormatInt(length, 10) + "e" +
			bstr("path") + "l" + bstr("file"+strconv.Itoa(i)+".mkv") + "e" + "e"
	}
	info := "d" +
		bstr("files") + "l" + entries + "e" +
		bstr("name") + bstr(name) +
		bstr("piece length") + "i16384e" +
		bstr("pieces") + bstr(strings.Repeat("P", 20)) + "e"
	sum := sha1.Sum([]byte(info))
	return []byte(torrentInfoDict(info)), hex.EncodeToString(sum[:])
}

func v2OnlyTorrent() []byte {
	info := "d" +
		bstr("file tree") + "de" +
		bstr("name") + bstr("v2 only") +
		bstr("piece length") + "i16384e" +
		bstr("meta version") + "i2e" + "e"
	return []byte(torrentInfoDict(info))
}

func TestTorrentInfoReadsSingleAndMultiFileMetainfo(t *testing.T) {
	body, want := singleFileTorrent("Test Movie 2026", 123456789)
	mt, e := TorrentInfo(body)
	if e != nil {
		t.Fatal(e)
	}
	if mt.Name != "Test Movie 2026" || mt.Size != 123456789 || mt.Files != 1 || mt.InfoHash != want || mt.Private {
		t.Fatal(mt, want)
	}
	body, want = multiFileTorrent("Test Pack", 1000, 2000, 3000)
	mt, e = TorrentInfo(body)
	if e != nil {
		t.Fatal(e)
	}
	if mt.Name != "Test Pack" || mt.Size != 6000 || mt.Files != 3 || mt.InfoHash != want {
		t.Fatal(mt, want)
	}
	private := strings.Replace(string(body), bstr("piece length"), bstr("private")+"i1e"+bstr("piece length"), 1)
	mt, e = TorrentInfo([]byte(private))
	if e != nil || !mt.Private {
		t.Fatal(mt, e)
	}
}

func TestTorrentInfoRejectsUnusableUploads(t *testing.T) {
	body, _ := singleFileTorrent("Test Movie 2026", 1)
	cases := []struct {
		name string
		data []byte
		code string
	}{
		{"empty", nil, "invalid_torrent"},
		{"truncated", body[:len(body)-3], "invalid_torrent"},
		{"trailing garbage", append(append([]byte{}, body...), 'x'), "invalid_torrent"},
		{"duplicate key", []byte("d" + bstr("x") + "i1e" + bstr("x") + "i2e" + "e"), "invalid_torrent"},
		{"v2 only", v2OnlyTorrent(), "unsupported_torrent"},
		{"no info", []byte("d" + bstr("announce") + bstr("http://t") + "e"), "invalid_torrent"},
		{"missing name", []byte("d" + bstr("info") + "d" + bstr("length") + "i1e" + bstr("pieces") + bstr(strings.Repeat("P", 20)) + "e" + "e"), "invalid_torrent"},
		{"bad name", []byte("d" + bstr("info") + "d" + bstr("length") + "i1e" + bstr("name") + "i9e" + bstr("pieces") + bstr(strings.Repeat("P", 20)) + "e" + "e"), "invalid_torrent"},
		{"no pieces", []byte("d" + bstr("info") + "d" + bstr("length") + "i1e" + bstr("name") + bstr("x") + "e" + "e"), "unsupported_torrent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, e := TorrentInfo(tc.data)
			var p *Error
			if !errors.As(e, &p) || p.Code != tc.code {
				t.Fatalf("want %s, got %v", tc.code, e)
			}
		})
	}
	oversized := make([]byte, 8<<20+1)
	if _, e := TorrentInfo(oversized); e == nil {
		t.Fatal("oversized upload accepted")
	}
}

func TestQueueManualTorrentTargetsMovieAndDeduplicatesByHash(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	body, hash := singleFileTorrent("Owned Manual Release 2026", 4096)
	job, created, e := f.s.QueueManualTorrent(ctx, f.movie.ID, "", body)
	if e != nil || !created {
		t.Fatal(job, created, e)
	}
	if job.Title != "Owned Manual Release 2026" || job.Hash != hash {
		t.Fatal(job, hash)
	}
	var title, storedHash, releaseID, protocol string
	if e = f.db.QueryRow(`SELECT title,info_hash,release_id,protocol FROM downloads WHERE info_hash=?`, hash).Scan(&title, &storedHash, &releaseID, &protocol); e != nil || title != "Owned Manual Release 2026" || releaseID != job.ID[:0]+"x" && releaseID == "" || protocol != "torrent" {
		t.Fatal(title, storedHash, releaseID, protocol, e)
	}
	if _, created, e = f.s.QueueManualTorrent(ctx, f.movie.ID, "", body); e == nil || created {
		var p *Error
		if !errors.As(e, &p) || p.Code != "duplicate_hash" {
			t.Fatal("duplicate upload accepted", e)
		}
	}
	if _, _, e = f.s.QueueManualTorrent(ctx, 999, "", body); e == nil {
		t.Fatal("unknown movie accepted")
	}
	if _, _, e = f.s.QueueManualTorrent(ctx, f.movie.ID, "replacement", body); e == nil {
		t.Fatal("replacement version accepted without a registered target")
	}
}

func TestQueueManualSeriesTorrentBindsSeasonTargets(t *testing.T) {
	f, id, targets := setupTV(t)
	ctx := context.Background()
	body, hash := multiFileTorrent("Test Show S01 Pack", 1024, 2048)
	job, created, e := f.s.QueueManualSeriesTorrent(ctx, id, 1, nil, body)
	if e != nil || !created {
		t.Fatal(job, created, e)
	}
	if job.Hash != hash || len(job.Targets) != len(targets) {
		t.Fatal(job, targets)
	}
	for i, target := range targets {
		if job.Targets[i].EpisodeID != target.EpisodeID {
			t.Fatal(job.Targets[i], target)
		}
	}
	single, created, e := f.s.QueueManualSeriesTorrent(ctx, id, 1, []int{2}, body)
	if e == nil || created {
		var p *Error
		if !errors.As(e, &p) || p.Code != "episode_busy" {
			t.Fatal("a second torrent for reserved episodes was not refused", single, e)
		}
	}
	if _, _, e = f.s.QueueManualSeriesTorrent(ctx, id, 2, nil, body); e == nil {
		t.Fatal("an unloaded season was accepted")
	}
	if _, _, e = f.s.QueueManualSeriesTorrent(ctx, id, 1, nil, nil); e == nil {
		t.Fatal("an empty upload was accepted")
	}
}
