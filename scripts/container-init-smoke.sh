#!/usr/bin/env bash
set -euo pipefail
image=${1:-reelscarey:ci}
docker run --rm -i --network none --user 0 --entrypoint python3 "$image" - <<'PY'
import os, pathlib, subprocess, tempfile
with tempfile.TemporaryDirectory(prefix="reelscarey-init-check-") as tmp:
    root=pathlib.Path(tmp)
    (root/'config').mkdir()
    (root/'outside').mkdir()
    owner=root/'outside'/'owner'
    owner.write_text('retained')
    os.chown(owner,2345,2345)
    (root/'config'/'external-link').symlink_to(owner)
    os.link(owner, root/'config'/'external-hardlink')
    def run(config, media='/media'):
        env=dict(os.environ, REELSCAREY_CONFIG_DIR=str(config), REELSCAREY_MEDIA_DIR=media)
        env.pop('REELSCAREY_BACKUPS_DIR',None)
        env.pop('REELSCAREY_YOUTUBE_STAGING_DIR',None)
        return subprocess.run(['python3','/usr/local/lib/reelscarey-init.py','1000','1000'],env=env).returncode
    assert run(root/'config')==0
    assert (root/'config').stat().st_uid==1000
    assert owner.stat().st_uid==2345 and owner.read_text()=='retained'
    (root/'linked').symlink_to(root/'outside')
    assert run(root/'linked')!=0
    assert run(root/'outside',str(root/'outside'))!=0
    assert owner.stat().st_uid==2345 and owner.read_text()=='retained'
print('Private initialization refuses linked/overlapping roots and preserves symbolic and hard-linked targets.')
PY