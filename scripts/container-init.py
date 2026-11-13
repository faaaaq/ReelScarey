"""Initialize only explicitly configured private app folders, then drop root."""
import os
import stat
import sys
from pathlib import Path

uid, gid = map(int, sys.argv[1:])
config = Path(os.environ.get("REELSCAREY_CONFIG_DIR") or "/config")
backups = Path(os.environ.get("REELSCAREY_BACKUPS_DIR") or config.parent / "backups")
# The whole staging volume is private app storage; the app creates its "work"
# subfolder inside it. Prepare the volume root so the volume is owned by the
# requested identity, not merely the subfolder.
staging = Path(os.environ.get("REELSCAREY_YOUTUBE_STAGING_DIR") or config.parent / "youtube-staging")
private = [config, backups, staging]
media = Path(os.environ.get("REELSCAREY_MEDIA_DIR") or "/media")
downloads = Path(os.environ.get("REELSCAREY_DOWNLOADS_DIR") or "/downloads")

def overlaps(a, b):
    return a == b or a in b.parents or b in a.parents

# Validate the complete layout before creating or changing anything. Never walk
# media/download mounts or a symlink to an unrelated host folder.
for folder in private:
    if not folder.is_absolute() or folder == Path("/") or folder.resolve() != folder:
        sys.exit("Private app folders must be absolute, distinct directories without symlinks.")
    if any(overlaps(folder, other.resolve()) for other in [media, downloads]):
        sys.exit("Private app folders must be separate from media and downloads.")
for index, folder in enumerate(private):
    if any(overlaps(folder, other) for other in private[index + 1:]):
        sys.exit("Configuration, backups and staging must use separate folders.")

# Optional single parent for all private folders (one host bind mount). Docker
# creates missing bind sources as root on the host, and a container can only
# fix ownership of what is mounted, so mounting one parent lets the whole
# private tree, including the parent itself, belong to PUID:PGID.
appdata_raw = os.environ.get("REELSCAREY_APPDATA_DIR")
if appdata_raw:
    appdata = Path(appdata_raw)
    if not appdata.is_absolute() or appdata == Path("/") or appdata.resolve() != appdata:
        sys.exit("REELSCAREY_APPDATA_DIR must be an absolute directory without symlinks.")
    if any(overlaps(appdata, other.resolve()) for other in [media, downloads]):
        sys.exit("REELSCAREY_APPDATA_DIR must be separate from media and downloads.")
    if any(appdata not in folder.parents for folder in private):
        sys.exit("Configuration, backups and staging must be inside REELSCAREY_APPDATA_DIR.")
    appdata.mkdir(parents=True, exist_ok=True)
    for folder in private:
        # Own every intermediate folder between the parent and each private
        # folder; never recurse into unrelated siblings.
        for level in [appdata, *[p for p in folder.parents if appdata in p.parents]]:
            level.mkdir(exist_ok=True)
            info = os.lstat(level)
            if stat.S_ISDIR(info.st_mode) and (info.st_uid != uid or info.st_gid != gid):
                os.chown(level, uid, gid, follow_symlinks=False)
    appdata.chmod(0o750)

for folder in private:
    folder.mkdir(parents=True, exist_ok=True)
    device = folder.stat().st_dev
    for parent, dirs, files, fd in os.fwalk(folder, follow_symlinks=False):
        # Directory descriptors keep ownership changes anchored if names change.
        dirs[:] = [name for name in dirs
                   if (info := os.stat(name, dir_fd=fd, follow_symlinks=False)).st_dev == device
                   and stat.S_ISDIR(info.st_mode)]
        for name in [".", *files]:
            info = os.stat(name, dir_fd=fd, follow_symlinks=False)
            if info.st_dev != device or stat.S_ISLNK(info.st_mode) or (stat.S_ISREG(info.st_mode) and info.st_nlink > 1):
                continue
            if info.st_uid != uid or info.st_gid != gid:
                os.chown(name, uid, gid, dir_fd=fd, follow_symlinks=False)
    folder.chmod(0o700)