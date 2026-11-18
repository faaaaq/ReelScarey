import hashlib, os, re, sys, collections

dirs = [sys.argv[1], sys.argv[2]]
out = sys.argv[3]

reqs = collections.defaultdict(set)
pattern = re.compile(r"^(.+?)-(.+)\.(tar\.gz|zip)$|^(.+)-(.+)-(.+)\.whl$")

for d in dirs:
    for name in sorted(os.listdir(d)):
        path = os.path.join(d, name)
        h = hashlib.sha256(open(path, "rb").read()).hexdigest()
        stem, ext = os.path.splitext(name)
        if ext == ".whl":
            # wheel: name-version-pytag-abitag-plattag
            parts = stem.split("-")
            pkg, ver = parts[0], parts[1]
        else:
            m = re.match(r"^(.+?)-(.+)$", stem)
            pkg, ver = m.group(1), m.group(2)
        pkg = pkg.replace("_", "-")
        reqs[f"{pkg}=={ver}"].add(f"sha256:{h}")

with open(out, "w", encoding="utf-8", newline="\n") as f:
    f.write("# Hash-pinned closure for the bundled TIDAL provider (tidaler).\n")
    f.write("# Generated from tidaler==0.1.8 with pip download for CPython 3.12 on\n")
    f.write("# manylinux2014 x86_64 and aarch64; pyaes and ratelimit ship as locally\n")
    f.write("# built universal wheels because PyPI only publishes their sdists.\n")
    f.write("# Regenerate by repeating that download after any version bump.\n")
    for key in sorted(reqs):
        f.write(f"{key} \\\n")
        for i, h in enumerate(sorted(reqs[key])):
            f.write(("    " if i else "  ") + f"--hash={h}" + (" \\\n" if i < len(reqs[key]) - 1 else "\n"))
print(f"wrote {out} with {len(reqs)} packages")
