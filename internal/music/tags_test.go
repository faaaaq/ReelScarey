package music

import "testing"

func TestParseAudioProbeReadsFormatTags(t *testing.T) {
	body := `{
		"format": {
			"format_name": "mp3",
			"duration": "213.500000",
			"bit_rate": "320000",
			"tags": {
				"artist": "Artist",
				"album_artist": "Album Artist",
				"album": "Album",
				"title": "Song",
				"track": "3/12",
				"disc": "1/2",
				"date": "2020-05-01",
				"musicbrainz_artistid": "a74b1b7f-71a5-4011-9441-d0b5e4122711",
				"musicbrainz_albumartistid": "11111111-1111-4111-8111-111111111112",
				"musicbrainz_albumid": "b1392450-e666-3926-a536-22c65f834433",
				"musicbrainz_releasegroupid": "b1392450-e666-3926-a536-22c65f834433",
				"musicbrainz_trackid": "65806a55-e874-3cde-92ba-8a5dc0dce42e",
				"musicbrainz_recordingid": "5229c9b3-8523-4b91-9a5f-c33e0c4eeb95"
			}
		},
		"streams": [{"codec_type": "audio", "sample_rate": "44100", "channels": 2}]
	}`
	tags, err := parseAudioProbe([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if tags.Format != "mp3" || tags.DurationMS != 213500 || tags.Bitrate != 320000 || tags.SampleRate != 44100 || tags.Channels != 2 {
		t.Fatal("stream facts", tags)
	}
	if tags.Artist != "Artist" || tags.AlbumArtist != "Album Artist" || tags.Album != "Album" || tags.Title != "Song" {
		t.Fatal("text tags", tags)
	}
	if tags.Track == nil || *tags.Track != 3 || tags.Disc == nil || *tags.Disc != 1 || tags.Year == nil || *tags.Year != 2020 {
		t.Fatal("number tags", tags)
	}
	if tags.MusicBrainzArtistID == "" || tags.MusicBrainzAlbumArtistID != "11111111-1111-4111-8111-111111111112" ||
		tags.MusicBrainzReleaseGroupID == "" || tags.MusicBrainzTrackID == "" || tags.MusicBrainzRecordingID == "" {
		t.Fatal("musicbrainz tags", tags)
	}
}

func TestParseAudioProbeReadsOGGStreamTags(t *testing.T) {
	body := `{
		"format": {"format_name": "ogg", "duration": "60.0", "tags": {}},
		"streams": [{
			"codec_type": "audio", "sample_rate": "48000", "channels": 2,
			"tags": {"artist": "OGG Artist", "album": "OGG Album", "title": "OGG Title", "tracknumber": "7", "discnumber": "2", "date": "1999"}
		}]
	}`
	tags, err := parseAudioProbe([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if tags.Artist != "OGG Artist" || tags.Album != "OGG Album" || tags.Title != "OGG Title" {
		t.Fatal("stream tags", tags)
	}
	if tags.Track == nil || *tags.Track != 7 || tags.Disc == nil || *tags.Disc != 2 || tags.Year == nil || *tags.Year != 1999 {
		t.Fatal("stream numbers", tags)
	}
	if tags.DurationMS != 60000 || tags.SampleRate != 48000 {
		t.Fatal("stream facts", tags)
	}
}

func TestParseAudioProbeRejectsInvalidBody(t *testing.T) {
	if _, err := parseAudioProbe([]byte(`not json`)); err == nil {
		t.Fatal("invalid body accepted")
	}
}
