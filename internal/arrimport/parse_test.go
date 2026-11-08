package arrimport

import (
	"archive/zip"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func backupZip(t *testing.T, name string, create func(*sql.DB)) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, name)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	create(db)
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "backup.zip")
	out, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = out.Close(); err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestParseRadarrAndSonarrBackups(t *testing.T) {
	radarr := backupZip(t, "radarr.db", func(db *sql.DB) {
		if _, err := db.Exec(`CREATE TABLE Movies(Id INTEGER PRIMARY KEY, TmdbId INTEGER, Title TEXT, Year INTEGER, Monitored INTEGER);
			INSERT INTO Movies(Id,TmdbId,Title,Year,Monitored) VALUES(1,11,'Released film',2020,1),(2,12,'Unreleased film',NULL,0),(3,0,'Ignored',NULL,1)`); err != nil {
			t.Fatal(err)
		}
	})
	backup, err := Parse(context.Background(), radarr)
	if err != nil {
		t.Fatal(err)
	}
	if backup.Source != "radarr" || len(backup.Items) != 2 {
		t.Fatal(backup)
	}
	if backup.Items[0].TMDbID != 11 || !backup.Items[0].Monitored || backup.Items[0].Title != "Released film" {
		t.Fatal(backup.Items[0])
	}
	if backup.Items[1].TMDbID != 12 || backup.Items[1].Monitored || backup.Items[1].Year != nil {
		t.Fatal(backup.Items[1])
	}

	sonarr := backupZip(t, "sonarr.db", func(db *sql.DB) {
		if _, err := db.Exec(`CREATE TABLE Series(Id INTEGER PRIMARY KEY, TvdbId INTEGER, Title TEXT, Year INTEGER, Monitored INTEGER);
			INSERT INTO Series(Id,TvdbId,Title,Year,Monitored) VALUES(1,100,'Show one',2019,1),(2,101,'Show two',2024,0)`); err != nil {
			t.Fatal(err)
		}
	})
	backup, err = Parse(context.Background(), sonarr)
	if err != nil {
		t.Fatal(err)
	}
	if backup.Source != "sonarr" || len(backup.Items) != 2 || backup.Items[0].TVDBID != 100 || backup.Items[1].Monitored {
		t.Fatal(backup)
	}
}

func TestParseRadarrMovieMetadataFallback(t *testing.T) {
	archive := backupZip(t, "radarr.db", func(db *sql.DB) {
		if _, err := db.Exec(`CREATE TABLE Movies(Id INTEGER PRIMARY KEY, MovieMetadataId INTEGER, Monitored INTEGER);
			CREATE TABLE MovieMetadata(Id INTEGER PRIMARY KEY, TmdbId INTEGER, Title TEXT, Year INTEGER);
			INSERT INTO Movies(Id,MovieMetadataId,Monitored) VALUES(1,1,1);
			INSERT INTO MovieMetadata(Id,TmdbId,Title,Year) VALUES(1,42,'Metadata film',2026)`); err != nil {
			t.Fatal(err)
		}
	})
	backup, err := Parse(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(backup.Items) != 1 || backup.Items[0].TMDbID != 42 || backup.Items[0].Title != "Metadata film" {
		t.Fatal(backup)
	}
}

func TestParseRejectsNonBackupZip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "backup.zip")
	out, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	if _, err = zw.Create("notes.txt"); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	out.Close()
	if _, err = Parse(context.Background(), archive); err == nil {
		t.Fatal("a ZIP without a database was accepted")
	}
}
