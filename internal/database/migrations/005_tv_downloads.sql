ALTER TABLE settings ADD COLUMN episode_naming TEXT NOT NULL DEFAULT 'standard' CHECK(episode_naming IN ('standard','with-title'));
ALTER TABLE release_candidates ADD COLUMN scope TEXT NOT NULL DEFAULT 'movie';
ALTER TABLE downloads ADD COLUMN kind TEXT NOT NULL DEFAULT 'movie' CHECK(kind IN ('movie','series'));
DROP INDEX one_movie_download;
CREATE UNIQUE INDEX one_movie_download ON downloads(movie_id) WHERE kind='movie' AND state NOT IN ('completed','cancelled');

CREATE TABLE download_tv (
    download_id TEXT PRIMARY KEY REFERENCES downloads(id),
    series_tmdb_id INTEGER NOT NULL,
    series_title TEXT NOT NULL,
    series_year INTEGER,
    season_number INTEGER NOT NULL,
    series_folder TEXT NOT NULL,
    naming TEXT NOT NULL CHECK(naming IN ('standard','with-title')),
    mapping_confirmed INTEGER NOT NULL DEFAULT 0 CHECK(mapping_confirmed IN (0,1))
);
CREATE TABLE download_targets (
    download_id TEXT NOT NULL REFERENCES download_tv(download_id),
    episode_id INTEGER NOT NULL REFERENCES episodes(id),
    tmdb_id INTEGER NOT NULL,
    season_number INTEGER NOT NULL,
    episode_number INTEGER NOT NULL,
    title TEXT NOT NULL,
    skipped INTEGER NOT NULL DEFAULT 0 CHECK(skipped IN (0,1)),
    PRIMARY KEY(download_id,episode_id)
);
CREATE TABLE episode_downloads (
    episode_id INTEGER PRIMARY KEY REFERENCES episodes(id),
    download_id TEXT NOT NULL REFERENCES download_tv(download_id)
);
CREATE INDEX episode_download_job ON episode_downloads(download_id);
CREATE TABLE download_tv_files (
    download_id TEXT NOT NULL REFERENCES download_tv(download_id),
    file_index INTEGER NOT NULL CHECK(file_index>=0),
    name TEXT NOT NULL,
    source_path TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK(size_bytes>0),
    PRIMARY KEY(download_id,file_index)
);
CREATE TABLE download_mappings (
    download_id TEXT NOT NULL,
    episode_id INTEGER NOT NULL,
    file_index INTEGER NOT NULL,
    PRIMARY KEY(download_id,episode_id),
    FOREIGN KEY(download_id,episode_id) REFERENCES download_targets(download_id,episode_id),
    FOREIGN KEY(download_id,file_index) REFERENCES download_tv_files(download_id,file_index)
);

CREATE TABLE import_operations_next (
    download_id TEXT NOT NULL REFERENCES downloads(id),
    file_index INTEGER NOT NULL DEFAULT -1,
    source_path TEXT NOT NULL,
    destination_path TEXT NOT NULL UNIQUE,
    stage_name TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK(size_bytes>0),
    source_mtime INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    probe_json TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT 'pending' CHECK(mode IN ('pending','hardlink','copy')),
    phase TEXT NOT NULL CHECK(phase IN ('planned','staged','published','committed')),
    updated_at INTEGER NOT NULL,
    PRIMARY KEY(download_id,file_index)
);
INSERT INTO import_operations_next(download_id,source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,probe_json,mode,phase,updated_at)
SELECT download_id,source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,probe_json,mode,phase,updated_at FROM import_operations;
DROP TABLE import_operations;
ALTER TABLE import_operations_next RENAME TO import_operations;

CREATE TABLE media_files_next (
    id INTEGER PRIMARY KEY,
    movie_id INTEGER UNIQUE REFERENCES media_items(id),
    series_id INTEGER REFERENCES media_items(id),
    download_id TEXT NOT NULL REFERENCES downloads(id),
    file_index INTEGER NOT NULL DEFAULT -1,
    relative_path TEXT NOT NULL UNIQUE,
    size_bytes INTEGER NOT NULL CHECK(size_bytes>0),
    sha256 TEXT NOT NULL,
    probe_json TEXT NOT NULL,
    imported_at INTEGER NOT NULL,
    CHECK((movie_id IS NOT NULL AND series_id IS NULL) OR (movie_id IS NULL AND series_id IS NOT NULL)),
    UNIQUE(download_id,file_index)
);
INSERT INTO media_files_next(id,movie_id,download_id,relative_path,size_bytes,sha256,probe_json,imported_at)
SELECT id,movie_id,download_id,relative_path,size_bytes,sha256,probe_json,imported_at FROM media_files;
DROP TABLE media_files;
ALTER TABLE media_files_next RENAME TO media_files;
CREATE TABLE episode_files (
    episode_id INTEGER PRIMARY KEY REFERENCES episodes(id),
    media_file_id INTEGER NOT NULL REFERENCES media_files(id)
);
CREATE INDEX episode_media_file ON episode_files(media_file_id);
