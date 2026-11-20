package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAudioCandidateEvaluationDoesNotBlindlyAcceptMissingDurationOrVersions(t *testing.T) {
	base := []AudioCandidate{
		{ID: "first", Title: "Artist - Song (Official Audio)", Channel: "Artist - Topic", DurationSeconds: 0},
		{ID: "second", Title: "Artist - Song (Official Audio)", Channel: "Artist - Topic", DurationSeconds: 0},
	}
	decision := evaluateAudioCandidates(base, AcquisitionTrackInput{Artist: "Artist", Title: "Song", DurationMS: 0})
	if decision.Chosen != nil || len(decision.Candidates) != 2 {
		t.Fatalf("missing duration made search order authoritative: %#v", decision)
	}
	decision = evaluateAudioCandidates(base[:1], AcquisitionTrackInput{Artist: "Artist", Title: "Song"})
	if decision.Chosen == nil || decision.Chosen.ID != "first" {
		t.Fatalf("a single otherwise-strong candidate with unknown duration should remain usable: %#v", decision)
	}
}

func TestAudioCandidateEvaluationSeparatesArtistVersionAndDuration(t *testing.T) {
	candidates := []AudioCandidate{
		{ID: "live", Title: "Artist - Song (Live)", Channel: "Artist - Topic", DurationSeconds: 200},
		{ID: "wrong", Title: "Other - Song", Channel: "Other - Topic", DurationSeconds: 200},
		{ID: "studio", Title: "Artist - Song (Official Audio)", Channel: "Artist - Topic", DurationSeconds: 200},
		{ID: "unknown", Title: "Artist - Song", Channel: "", DurationSeconds: 0},
	}
	decision := evaluateAudioCandidates(candidates, AcquisitionTrackInput{Artist: "Artist", Title: "Song", DurationMS: 200000})
	if decision.Chosen == nil || decision.Chosen.ID != "studio" {
		t.Fatalf("studio/artist/duration evidence was not ranked: %#v", decision)
	}
	decision = evaluateAudioCandidates(candidates, AcquisitionTrackInput{Artist: "Artist", Title: "Song (Live)", DurationMS: 200000})
	if decision.Chosen == nil || decision.Chosen.ID != "live" {
		t.Fatalf("requested live version was not retained: %#v", decision)
	}
	for _, qualifier := range []string{"cover", "remix", "instrumental", "radio edit", "sped up"} {
		variant := []AudioCandidate{{ID: qualifier, Title: "Artist - Song (" + qualifier + ")", Channel: "Artist - Topic", DurationSeconds: 200}}
		decision = evaluateAudioCandidates(variant, AcquisitionTrackInput{Artist: "Artist", Title: "Song", DurationMS: 200000})
		if decision.Chosen != nil {
			t.Fatalf("%s was accepted as an unspecified studio recording", qualifier)
		}
		decision = evaluateAudioCandidates(variant, AcquisitionTrackInput{Artist: "Artist", Title: "Song (" + qualifier + ")", DurationMS: 200000})
		if decision.Chosen == nil || decision.Chosen.ID != qualifier {
			t.Fatalf("explicit %s request was not honored: %#v", qualifier, decision)
		}
	}
}

func TestAudioCandidateEvaluationRefusesEqualEvidence(t *testing.T) {
	candidates := []AudioCandidate{
		{ID: "b", Title: "Artist - Song", Channel: "Artist - Topic", DurationSeconds: 200},
		{ID: "a", Title: "Artist - Song", Channel: "Artist - Topic", DurationSeconds: 200},
	}
	decision := evaluateAudioCandidates(candidates, AcquisitionTrackInput{Artist: "Artist", Title: "Song", DurationMS: 200000})
	if decision.Chosen != nil || decision.Reason == "" {
		t.Fatal("equal candidates were resolved by search order")
	}
}

func TestAudioCandidateEvaluationAcceptsReleaseVariantTitles(t *testing.T) {
	// Real-world shape captured from a live failure: the provider title
	// carries "(Remastered)" while no YouTube upload of the same recording
	// does. The official channel upload must be chosen instead of failing
	// every candidate, and remix/live performances must stay ineligible.
	candidates := []AudioCandidate{
		{ID: "official", Title: "Copacabana (At the Copa) (Short Version)", Channel: "Barry Manilow", DurationSeconds: 237},
		{ID: "reupload", Title: "Barry Manilow - Copacabana (At the Copa) (Long Version)", Channel: "Top Squad", DurationSeconds: 345},
		{ID: "remix", Title: "Barry Manilow - Copacabana (At the Copa) ((Remix)(Official Video))", Channel: "Barry Manilow", DurationSeconds: 241},
		{ID: "live", Title: "Barry Manilow - Copacabana (Live from The 1978 BBC Special)", Channel: "Barry Manilow", DurationSeconds: 481},
	}
	decision := evaluateAudioCandidates(candidates, AcquisitionTrackInput{Title: "Copacabana (Remastered)", Artist: "Barry Manilow", DurationMS: 245000})
	if decision.Chosen == nil || decision.Chosen.ID != "official" {
		t.Fatalf("release-variant request was not matched to the official recording: %#v", decision)
	}
	for _, evidence := range decision.Candidates {
		if (evidence.Candidate.ID == "remix" || evidence.Candidate.ID == "live") && evidence.Eligible {
			t.Fatalf("%s must stay ineligible: %#v", evidence.Candidate.ID, evidence)
		}
	}
	// When a candidate does carry the requested release variant it outranks
	// the plain recording.
	tagged := append(append([]AudioCandidate{}, candidates...), AudioCandidate{ID: "tagged", Title: "Copacabana (Remastered)", Channel: "Barry Manilow", DurationSeconds: 245})
	decision = evaluateAudioCandidates(tagged, AcquisitionTrackInput{Title: "Copacabana (Remastered)", Artist: "Barry Manilow", DurationMS: 245000})
	if decision.Chosen == nil || decision.Chosen.ID != "tagged" {
		t.Fatalf("explicit remaster tag was not preferred: %#v", decision)
	}
	// The reverse direction: a plain catalog title may be satisfied by the
	// remastered official upload of the same recording.
	decision = evaluateAudioCandidates([]AudioCandidate{{ID: "remaster", Title: "Artist - Song (Remastered 2011)", Channel: "Artist - Topic", DurationSeconds: 200}},
		AcquisitionTrackInput{Title: "Song", Artist: "Artist", DurationMS: 200000})
	if decision.Chosen == nil || decision.Chosen.ID != "remaster" {
		t.Fatalf("plain request refused a remastered upload of the same recording: %#v", decision)
	}
}

type evidenceAudioSource struct {
	candidates []AudioCandidate
	downloads  int
}

func (s *evidenceAudioSource) SearchAudio(context.Context, string, int) ([]AudioCandidate, error) {
	return s.candidates, nil
}
func (s *evidenceAudioSource) DownloadAudio(_ context.Context, _ string, directory, _ string) (AudioFile, error) {
	s.downloads++
	path := filepath.Join(directory, "track.mp3")
	if err := os.WriteFile(path, []byte("audio"), 0600); err != nil {
		return AudioFile{}, err
	}
	return AudioFile{Path: path, Format: "mp3"}, nil
}
func (s *evidenceAudioSource) InspectAudio(context.Context, string) (AudioInspection, error) {
	return AudioInspection{}, errors.New("not used")
}

func TestAcquisitionPersistsCandidateEvidenceWithoutDownloadingAmbiguity(t *testing.T) {
	service, db, _ := editFixture(t)
	service.SetDownloadsDir(t.TempDir())
	source := &evidenceAudioSource{candidates: []AudioCandidate{
		{ID: "a", Title: "Artist - Song", Channel: "Artist - Topic", DurationSeconds: 200},
		{ID: "b", Title: "Artist - Song", Channel: "Artist - Topic", DurationSeconds: 200},
	}}
	service.SetAudioSource(source)
	job, err := service.StartAcquisition(context.Background(), "", "top_track", "Song", "Music", "source", []AcquisitionTrackInput{{
		Title: "Song", Artist: "Artist", Album: "Album", TrackNumber: 1, DiscNumber: 1, DurationMS: 200000, SearchQuery: "Artist Song",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessAcquisition(context.Background()); err != nil || !worked {
		t.Fatal(worked, err)
	}
	if source.downloads != 0 {
		t.Fatal("ambiguous candidates reached download")
	}
	reopened := NewService(db, t.TempDir())
	saved, err := reopened.Acquisition(context.Background(), job.ID)
	if err != nil || len(saved.Tracks) != 1 || len(saved.Tracks[0].CandidateEvidence) != 2 {
		t.Fatalf("candidate evidence did not survive reopen: %#v %v", saved, err)
	}
	if saved.Tracks[0].Error == "" {
		t.Fatal("ambiguity was not explained")
	}
}

func TestTargetedRetryPreservesOtherTracksAndAttemptHistory(t *testing.T) {
	service, db, _ := editFixture(t)
	service.SetDownloadsDir(t.TempDir())
	source := &evidenceAudioSource{}
	service.SetAudioSource(source)
	job, err := service.StartAcquisition(context.Background(), "", "top_track", "Songs", "Music", "source", []AcquisitionTrackInput{
		{Title: "First", Artist: "Artist", Album: "Album", TrackNumber: 1, DiscNumber: 1, SearchQuery: "Artist First"},
		{Title: "Second", Artist: "Artist", Album: "Album", TrackNumber: 2, DiscNumber: 1, SearchQuery: "Artist Second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var firstID, secondID int64
	if err = db.QueryRow(`SELECT id FROM music_acquisition_tracks WHERE job_id=? AND position=1`, job.ID).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT id FROM music_acquisition_tracks WHERE job_id=? AND position=2`, job.ID).Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE music_acquisition_tracks SET state='failed',error='bad source',source_url='old-source' WHERE id=?`, firstID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE music_acquisition_tracks SET state='completed' WHERE id=?`, secondID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_acquisition_attempts(track_id,attempted_at,source_id,source_url,outcome) VALUES(?,1,'old-source','old-source','bad source')`, firstID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE music_acquisition_jobs SET state='failed' WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := service.RetryAcquisitionTrack(context.Background(), job.ID, firstID, "replacement-source", true)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Tracks[0].State != "queued" || retried.Tracks[0].SourceURL != "replacement-source" || retried.Tracks[1].State != "completed" || len(retried.Tracks[0].Attempts) != 1 || retried.Tracks[0].Attempts[0].SourceID != "old-source" {
		t.Fatalf("targeted retry changed the wrong state or lost history: %#v", retried.Tracks)
	}
	if worked, err := service.ProcessAcquisition(context.Background()); err != nil || !worked {
		t.Fatal(worked, err)
	}
	if source.downloads != 1 {
		t.Fatalf("replacement attempt did not reach download exactly once: %d", source.downloads)
	}
	saved, err := service.Acquisition(context.Background(), job.ID)
	if err != nil || len(saved.Tracks[0].Attempts) != 2 || saved.Tracks[0].Attempts[1].SourceID != "replacement-source" {
		t.Fatalf("new attempt did not append to history: %#v %v", saved.Tracks[0].Attempts, err)
	}
}

func TestAcquisitionReusesFileRegisteredAfterQueueAndKeepsExactEdition(t *testing.T) {
	service, db, _ := editFixture(t)
	service.SetDownloadsDir(t.TempDir())
	source := &evidenceAudioSource{}
	service.SetAudioSource(source)
	job, err := service.StartAlbumAcquisition(context.Background(), 2, "Music", "source")
	if err != nil {
		t.Fatal(err)
	}
	insertMusicFile(t, db, 10, 3, "Music/Artist/Album/01 Song.flac", matchingTags())
	if worked, err := service.ProcessAcquisition(context.Background()); err != nil || !worked {
		t.Fatal(worked, err)
	}
	if source.downloads != 0 || len(source.candidates) != 0 {
		t.Fatal("existing local recording was downloaded again")
	}
	saved, err := service.Acquisition(context.Background(), job.ID)
	if err != nil || len(saved.Tracks) != 1 || saved.Tracks[0].State != "completed" || saved.Tracks[0].MusicFileID == nil || *saved.Tracks[0].MusicFileID != 10 {
		t.Fatalf("local file was not reused: %#v %v", saved, err)
	}
	track := saved.Tracks[0]
	if track.TargetAlbumID != 2 || track.TargetTrackID != 3 || track.RecordingID != testRecordingID || track.TrackMBID != testTrackID || track.ReleaseGroupID != testRGID {
		t.Fatalf("requested identity was not persisted: %#v", track)
	}
	if saved.SourceURL != "https://musicbrainz.org/release-group/"+testRGID {
		t.Fatalf("catalog source missing: %q", saved.SourceURL)
	}
	albumID, trackID, err := service.ensureAcquiredTrack(context.Background(), track)
	if err != nil || albumID != 2 || trackID != 3 {
		t.Fatalf("exact edition target changed: %d %d %v", albumID, trackID, err)
	}
}
