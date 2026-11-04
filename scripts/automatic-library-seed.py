"""Owned fixtures only: this runs inside a fresh isolated acceptance container."""
import hashlib
import json
import pathlib
import shutil
import sqlite3
import subprocess
import sys
import struct

root = pathlib.Path('/media')
db = sqlite3.connect('/config/reelscarey.db')
if '--verify' in sys.argv:
    evidence = json.loads(pathlib.Path('/config/automatic-fixture.json').read_text())
    for name, expected in evidence.items():
        file = root / name
        actual = [hashlib.sha256(file.read_bytes()).hexdigest(), file.stat().st_mtime_ns, file.stat().st_ino]
        assert actual == expected, name
    assert db.execute('SELECT COUNT(*) FROM media_files').fetchone()[0] == 5
    assert db.execute('SELECT COUNT(*) FROM episode_files').fetchone()[0] == 3
    assert db.execute('SELECT COUNT(*) FROM downloads').fetchone()[0] == 0
    assert db.execute("SELECT COUNT(*) FROM library_match_candidates WHERE state='review'").fetchone()[0] == 1
    assert db.execute("SELECT enabled FROM movie_version_settings WHERE media_id=3").fetchone()[0] == 1
    assert db.execute("SELECT video_resolution FROM media_files WHERE movie_id=3 ORDER BY video_resolution").fetchall() == [(1080,), (2160,)]
    for relative, sha, probe, observation in db.execute('SELECT relative_path,sha256,probe_json,verification_json FROM media_files'):
        assert sha == '', 'large fixture should use fast observation'
        observed = json.loads(observation)
        assert observed['size'] == (root / relative).stat().st_size
        assert observed['mtime'] == (root / relative).stat().st_mtime_ns
        assert len(observed['sample']) == 64
        facts = json.loads(probe)
        width = 3840 if '2160p' in relative else 1920 if '1080p' in relative else 160
        assert facts['codec'] == 'h264' and facts['width'] == width
    assert db.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
    assert db.execute('PRAGMA foreign_key_check').fetchall() == []
    print('Automatic movie/TV registration verified: real FFprobe, concurrent HD/4K movie files, retained version policy, three canonical episodes, source bytes/inodes/timestamps preserved, no fabricated downloads.')
    sys.exit(0)

names = ['Movies/Fixture Movie (2026)/Fixture.Movie.2026.mp4',
         'Movies/Duplicate Movie (2026)/Duplicate.Movie.2026.1080p.SDR.mp4',
         'Movies/Duplicate Movie (2026)/Duplicate.Movie.2026.2160p.HDR.mp4',
         'Movies/Uncertain.mp4',
         'TV Shows/Fixture Show (2026)/Season 01/S01E01.mp4',
         'TV Shows/Fixture Show (2026)/Season 01/S01E02-E03.mp4']
evidence = {}
for index, name in enumerate(names):
    file = root / name
    file.parent.mkdir(parents=True, exist_ok=True)
    if index <= 2:
        size = '3840x2160' if index == 2 else '1920x1080' if index == 1 else '160x90'
        subprocess.run(['ffmpeg','-v','error','-f','lavfi','-i',f'color=c=blue:s={size}:r=1','-frames:v','2','-c:v','libx264','-preset','ultrafast','-threads','1','-pix_fmt','yuv420p',str(file)],check=True)
        # Place the metadata after a sparse padding box. Inspection must seek,
        # and registration must retain a bounded observation across restart.
        data = file.read_bytes()
        offset = 0
        while data[offset+4:offset+8] != b'moov':
            size = struct.unpack('>I', data[offset:offset+4])[0]
            assert size >= 8
            offset += size
        assert offset + struct.unpack('>I', data[offset:offset+4])[0] == len(data)
        padding = 1 << 20
        with file.open('r+b') as handle:
            handle.seek(offset)
            handle.write(struct.pack('>I4s', padding, b'free'))
            handle.seek(offset + padding)
            handle.write(data[offset:])
    else:
        shutil.copyfile(root / names[0],file)
    evidence[name] = [hashlib.sha256(file.read_bytes()).hexdigest(),file.stat().st_mtime_ns,file.stat().st_ino]
for key, kind, title in [(1,'movie','Fixture Movie'),(2,'series','Fixture Show'),(3,'movie','Duplicate Movie')]:
    db.execute('INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored) VALUES(?,?,?,?,2026,0)',(key,kind,99000+key,title))
db.execute('INSERT INTO series_details(media_id) VALUES(2)')
db.execute("INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at) VALUES(1,2,99010,1,'Season 1','','',3,'fixture')")
for number in range(1,4):
    db.execute("INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(?,1,?,?,?,'','')",(number,99020+number,number,'Episode '+str(number)))
db.commit()
pathlib.Path('/config/automatic-fixture.json').write_text(json.dumps(evidence))
print('Disposable automatic-matching fixtures generated.')
