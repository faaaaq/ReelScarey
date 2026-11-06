package announce

import (
	"strings"
	"testing"
)

func TestDefinitionsCompile(t *testing.T) {
	defs, e := Definitions()
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"iptorrents", "torrentleech", "filelist", "beyondhd", "animebytes", "myanonamouse"} {
		if _, ok := defs[id]; !ok {
			t.Fatalf("definition %s missing", id)
		}
	}
}

func TestDefinitionsParseRealAnnounceSamples(t *testing.T) {
	defs, e := Definitions()
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		tracker string
		line    string
		name    string
		id      string
		info    string
	}{
		{"iptorrents", `[Movies/XviD] The Movie 2010 DVDRip XviD-GROUP FREELEECH - http://www.iptorrents.com/details.php?id=000000 - 7.63 GB`, "The Movie 2010 DVDRip XviD-GROUP", "000000", "http://www.iptorrents.com/details.php?id=000000"},
		{"torrentleech", `New Torrent Announcement: <PC :: Iso>  Name:'debian live 10 6 0 amd64 standard iso' uploaded by 'Anonymous' freeleech -  http://www.tracker01.test/torrent/000000`, "debian live 10 6 0 amd64 standard iso", "000000", "http://www.tracker01.test/torrent/000000"},
		{"filelist", `New Torrent: This.New.Movie.2021.1080p.Blu-ray.AVC.DTS-HD.MA.5.1-BEATRIX -- [FreeLeech!] -- [Filme Blu-Ray] [26.78 GB] -- https://filelist.io/details.php?id=000000 -- by uploader1`, "This.New.Movie.2021.1080p.Blu-ray.AVC.DTS-HD.MA.5.1-BEATRIX", "000000", "https://filelist.io/details.php?id=000000"},
		{"beyondhd", `New Torrent: That Show S03E04 1080p WEB-DL DDP 5.1 H.264-GROUP [Capped FL] Category: TV By: Anonymous Size: 1.62 GiB Link: https://beyond-hd.me/torrents/autodl?id=00000`, "That Show S03E04 1080p WEB-DL DDP 5.1 H.264-GROUP", "00000", "https://beyond-hd.me/torrents/autodl?id=00000"},
		{"animebytes", `Great BluRay SoftSubbed Anime - TV Series  [2020] :: Blu-ray / MKV / h264 10-bit / 1080p / FLAC 2.0 / Dual Audio / Softsubs (Sub Group) / Freeleech || https://animebytes.tv/torrents.php?id=00008&torrentid=000000 || comedy, drama, school.life, sports || Uploaded by: Uploader`, "Great BluRay SoftSubbed Anime - TV Series", "000000", "https://animebytes.tv/torrents.php?id=00008&torrentid=000000"},
		{"myanonamouse", `Frost Hunger By: Indigo Frey [English] [Ebook] [Fiction] [epub] [1.07 MiB] - Fantasy, LGBTQIA+, Erotica/Sexual Content, Romance - https://www.myanonamouse.net/t/1262654 VIP`, "Frost Hunger By: Indigo Frey [English] [Ebook] [Fiction] [epub]", "1262654", "https://www.myanonamouse.net/t/1262654"},
	}
	for _, tc := range cases {
		t.Run(tc.tracker, func(t *testing.T) {
			parsed, ok := defs[tc.tracker].Parse(tc.line)
			if !ok {
				t.Fatal("line was not parsed")
			}
			if parsed.ReleaseName != tc.name || parsed.TorrentID != tc.id || parsed.InfoURL != tc.info {
				t.Fatal(parsed, tc.name, tc.id, tc.info)
			}
			if defs[tc.tracker].ID != "myanonamouse" && defs[tc.tracker].ID != "torrentleech" && defs[tc.tracker].ID != "animebytes" && parsed.SizeBytes <= 0 {
				t.Fatal("size was not parsed", parsed)
			}
		})
	}
}

func TestDefinitionsIgnoreChatter(t *testing.T) {
	defs, e := Definitions()
	if e != nil {
		t.Fatal(e)
	}
	for id, d := range defs {
		if _, ok := d.Parse("Welcome to the network, enjoy your stay"); ok {
			t.Fatalf("%s parsed unrelated chatter", id)
		}
	}
	if !strings.Contains(defs["iptorrents"].String(), "IPTorrents") {
		t.Fatal(defs["iptorrents"].String())
	}
}

func TestParseSize(t *testing.T) {
	cases := []struct {
		raw    string
		binary bool
		want   int64
	}{
		{"7.63 GB", true, 8192650117},
		{"7.63 GB", false, 7630000000},
		{"1.07 MiB", false, 1121976},
		{"0", false, 0},
	}
	for _, tc := range cases {
		if got := parseSize(tc.raw, tc.binary); got != tc.want {
			t.Fatalf("%q binary=%v: got %d want %d", tc.raw, tc.binary, got, tc.want)
		}
	}
}
