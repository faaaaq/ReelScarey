"""Generate owned media and seed only the fresh acceptance container's catalog."""
import hashlib
import pathlib
import shutil
import sqlite3
import subprocess
import sys

if '--verify' in sys.argv:
    db = sqlite3.connect('/config/reelscarey.db')
    for relative, sha in db.execute('SELECT relative_path,sha256 FROM media_files'):
        assert hashlib.sha256((pathlib.Path('/media') / relative).read_bytes()).hexdigest() == sha
    current = pathlib.Path('/media/Movies/Fixture Film/Fixture Film (2026).en.srt').read_text()
    assert 'Reviewed generated replacement.' in current
    history = list(pathlib.Path('/media/.reelscarey-subtitle-history').iterdir())
    assert len(history) == 3
    assert any('Generated acceptance cue.' in item.read_text() for item in history)
    assert db.execute('SELECT COUNT(*) FROM media_files').fetchone()[0] == 3
    assert db.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
    assert db.execute('PRAGMA foreign_key_check').fetchall() == []
    print('All generated video hashes preserved; subtitle originals retained; catalog integrity verified.')
    sys.exit(0)

movie = pathlib.Path('/media/Movies/Fixture Film/Fixture Film (2026).mp4')
movie.parent.mkdir(parents=True)
subprocess.run(['ffmpeg', '-v', 'error', '-f', 'lavfi', '-i', 'color=c=blue:s=160x90:r=10',
                '-t', '2', '-c:v', 'libx264', '-pix_fmt', 'yuv420p', str(movie)], check=True)
subtitle = movie.with_suffix('.en.srt')
subtitle.write_text('1\n00:00:00,500 --> 00:00:01,500\nGenerated acceptance cue.\n', encoding='utf8')
for title in ['Bulk Alpha', 'Bulk Beta']:
    target = pathlib.Path('/media/Movies') / title / (title + ' (2026).mp4')
    target.parent.mkdir(parents=True)
    shutil.copyfile(movie, target)
db = sqlite3.connect('/config/reelscarey.db')
for key, kind, title in [(1, 'movie', 'Fixture Film'), (2, 'series', 'Fixture Show'),
                         (3, 'movie', 'Bulk Alpha'), (4, 'movie', 'Bulk Beta')]:
    db.execute('INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored) VALUES(?,?,?,?,2026,0)', (key, kind, 900000+key, title))
db.execute("INSERT INTO series_details(media_id,series_status) VALUES(2,'Returning Series')")
db.execute("INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(1,1,?,?,?,'{}',unixepoch(),'existing')",
           (str(movie.relative_to('/media')), movie.stat().st_size, hashlib.sha256(movie.read_bytes()).hexdigest()))
db.execute("INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,updated_at) VALUES(1,'en','full','existing',?,?,unixepoch())",
           (str(subtitle.relative_to('/media')), hashlib.sha256(subtitle.read_bytes()).hexdigest()))
db.commit()
print('Disposable generated media and catalog ready.')
