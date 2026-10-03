#!/usr/bin/env python3
"""Build Urth release archives and packages without publishing or changing Git refs."""

import argparse
import datetime
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
COMPONENTS = {
    "api-server": ("linux",),
    "nats-worker": ("linux",),
    "urthctl": ("linux", "darwin"),
}
ARCHES = ("amd64", "arm64")
# Image tags cannot contain SemVer build metadata (+...). Numeric prerelease
# identifiers must not have leading zeroes. Limit the total Docker tag length.
VERSION = re.compile(
    r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
    r"(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?"
)


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def metadata(version):
    commit = git("rev-parse", "HEAD")
    epoch = int(git("show", "-s", "--format=%ct", "HEAD"))
    dirty = bool(git("status", "--porcelain"))
    if version:
        if len(version) > 128 or not VERSION.fullmatch(version):
            raise ValueError("version must be vMAJOR.MINOR.PATCH[-PRERELEASE], without build metadata")
        if dirty:
            raise ValueError("versioned builds require a clean worktree")
        if git("rev-parse", f"refs/tags/{version}^{{commit}}") != commit:
            raise ValueError("version tag must name HEAD")
    else:
        version = "snapshot-" + commit[:12]
    return {
        "schema": 1,
        "version": version,
        "revision": commit,
        "created": datetime.datetime.fromtimestamp(epoch, datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "source": "https://github.com/sre-norns/urth",
        "snapshot": version.startswith("snapshot-"),
        "dirty": dirty,
        "epoch": epoch,
        "stable": not version.startswith("snapshot-") and "-" not in version,
    }


def encoded(value):
    return (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()


def archive(destination, entries, epoch):
    # Fixed ownership, permissions, ordering and time keep snapshots repeatable.
    # Only the explicit binary/docs/assets list enters the archive.
    with destination.open("wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=epoch) as zipped:
            with tarfile.open(fileobj=zipped, mode="w") as bundle:
                for name, (data, mode) in sorted(entries.items()):
                    info = tarfile.TarInfo(name)
                    info.size, info.mode, info.mtime = len(data), mode, epoch
                    bundle.addfile(info, io.BytesIO(data))


def checksums(output):
    entries = []
    for path in sorted(output.iterdir()):
        if path.is_file() and path.name != "checksums.txt":
            entries.append(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n")
    (output / "checksums.txt").write_text("".join(entries))


def records(directory, info):
    # GoReleaser places each platform's record in its archive as release.json.
    for component, systems in COMPONENTS.items():
        for system in systems:
            for arch in ARCHES:
                path = directory / component / f"{system}_{arch}" / "release.json"
                path.parent.mkdir(parents=True)
                path.write_bytes(encoded(dict(info, component=component, os=system, arch=arch)))


def goreleaser(info, records_dir, snap):
    # GoReleaser compiles every binary and builds the archives, Debian packages
    # and snaps. It never publishes here; the release workflow does that.
    command = shlex.split(os.environ.get("GORELEASER", "goreleaser")) + ["release", "--clean"]
    skip = [] if info["snapshot"] else ["publish", "announce"]
    if info["snapshot"]:
        command.append("--snapshot")
    if not snap:
        skip.append("snapcraft")
    if skip:
        command.append("--skip=" + ",".join(skip))
    env = dict(os.environ, RELEASE_VERSION=info["version"], RELEASE_RECORDS=str(records_dir), GOWORK="off")
    subprocess.run(command, cwd=ROOT, env=env, check=True)
    built = ROOT / "dist" / "goreleaser"
    return sorted(path for path in built.iterdir() if path.is_file() and path.name.endswith((".tar.gz", ".deb", ".snap")))


def build(output, website, info, snap=True):
    if output.exists() and any(output.iterdir()):
        raise ValueError("output directory must be empty; use a new snapshot directory")
    if not (website / "index.html").is_file():
        raise ValueError("website assets are missing; run npm ci and npm run build in website first")
    asset_entries = {}
    for path in sorted(website.rglob("*")):
        if path.is_symlink() or (not path.is_dir() and not path.is_file()):
            raise ValueError("website assets must contain only regular files and directories")
        if path.is_file():
            asset_entries["website/" + path.relative_to(website).as_posix()] = (path.read_bytes(), 0o644)
    output.mkdir(parents=True, exist_ok=True)
    info = dict(info, toolchain=subprocess.check_output(["go", "version"], text=True).strip())
    with tempfile.TemporaryDirectory(prefix="urth-release-") as staging:
        records(Path(staging) / "records", info)
        built = goreleaser(info, Path(staging) / "records", snap)
        staged = Path(staging) / "archives"
        staged.mkdir()
        for path in built:
            shutil.copy2(path, staged / path.name)
        archives = sorted(path.name for path in built if path.name.endswith(".tar.gz"))
        packages = sorted(path.name for path in built if not path.name.endswith(".tar.gz"))
        name = f"urth-website_{info['version']}.tar.gz"
        asset_entries.update({
            "LICENSE": ((ROOT / "LICENSE").read_bytes(), 0o644),
            "README.md": ((ROOT / "website" / "README.md").read_bytes(), 0o644),
            "release.json": (encoded(dict(info, component="urth-website")), 0o644),
        })
        archive(staged / name, asset_entries, info["epoch"])
        archives.append(name)
        for released in archives + packages:
            print(released, flush=True)
        (staged / "release-manifest.json").write_bytes(encoded(dict(info, archives=archives, packages=packages, snap=snap)))
        checksums(staged)
        # Do not dirty Git while later binaries still capture VCS metadata,
        # even when a caller selects an unignored output directory in the repo.
        for path in staged.iterdir():
            shutil.move(str(path), output / path.name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--snapshot", action="store_true", help="build local snapshot archives; never publish")
    mode.add_argument("--version", help="existing vSemVer tag at clean HEAD; never create or push a tag")
    parser.add_argument("--metadata-only", action="store_true", help="validate the source/version and print JSON without building")
    parser.add_argument("--output", type=Path, default=ROOT / "dist" / "release")
    parser.add_argument("--website-dist", type=Path, default=ROOT / "website" / "dist")
    parser.add_argument("--no-snap", action="store_true", help="skip snaps when snapcraft is unavailable; the manifest records it")
    args = parser.parse_args()
    try:
        info = metadata(args.version)
        if args.metadata_only:
            print(encoded(info).decode(), end="")
        else:
            build(args.output.resolve(), args.website_dist.resolve(), info, snap=not args.no_snap)
    except (ValueError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"release build refused: {error}\n")


if __name__ == "__main__":
    main()
