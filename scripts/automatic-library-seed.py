"""Owned fixtures only: this runs inside a fresh isolated acceptance container."""
import hashlib
import json
import pathlib
import shutil
import sqlite3
import subprocess
import sys

root = pathlib.Path('/media')
db = sqlite3.connect('/config/reelscarey.db')
if '--verify' in sys.argv:
    evidence = json.loads(pathlib.Path('/config/automatic-fixture.json').read_text())
    for name, expected in evidence.items():
        file = root / name
        actual = [hashlib.sha256(file.read_bytes()).hexdigest(), file.stat().st_mtime_ns, file.stat().st_ino]
        assert actual == expected, name
    assert db.execute('SELECT COUNT(*) FROM media_files').fetchone()[0] == 3
    assert db.execute('SELECT COUNT(*) FROM episode_files').fetchone()[0] == 3
    assert db.execute('SELECT COUNT(*) FROM downloads').fetchone()[0] == 0
    assert db.execute("SELECT COUNT(*) FROM library_match_candidates WHERE state='review'").fetchone()[0] == 3
    for relative, sha, probe in db.execute('SELECT relative_path,sha256,probe_json FROM media_files'):
        assert hashlib.sha256((root / relative).read_bytes()).hexdigest() == sha
        facts = json.loads(probe)
        assert facts['codec'] == 'h264' and facts['width'] == 160
    assert db.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
    assert db.execute('PRAGMA foreign_key_check').fetchall() == []
    print('Automatic movie/TV registration verified: real FFprobe, three canonical episodes, duplicate exceptions, source bytes/inodes/timestamps preserved, no fabricated downloads.')
    sys.exit(0)

names = ['Movies/Fixture Movie (2026)/Fixture.Movie.2026.mp4',
         'Movies/Duplicate Movie (2026)/Duplicate.Movie.2026.720p.mp4',
         'Movies/Duplicate Movie (2026)/Duplicate.Movie.2026.1080p.mp4',
         'Movies/Uncertain.mp4',
         'TV Shows/Fixture Show (2026)/Season 01/S01E01.mp4',
         'TV Shows/Fixture Show (2026)/Season 01/S01E02-E03.mp4']
evidence = {}
for index, name in enumerate(names):
    file = root / name
    file.parent.mkdir(parents=True, exist_ok=True)
    if index == 0:
        subprocess.run(['ffmpeg','-v','error','-f','lavfi','-i','color=c=blue:s=160x90:r=10','-t','2','-c:v','libx264','-pix_fmt','yuv420p',str(file)],check=True)
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
