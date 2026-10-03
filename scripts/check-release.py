#!/usr/bin/env python3
"""Inspect release source records, checksums, archive members and native help."""

import argparse
import hashlib
import json
from pathlib import Path
import platform
import subprocess
import tarfile
import tempfile


def check(output, smoke):
    manifest = json.loads((output / "release-manifest.json").read_text())
    expected = {
        f"{component}_{manifest['version']}_{system}_{arch}.tar.gz"
        for component, systems in {"api-server": ("linux",), "nats-worker": ("linux",), "urthctl": ("linux", "darwin")}.items()
        for system in systems for arch in ("amd64", "arm64")
    }
    expected.add(f"urth-website_{manifest['version']}.tar.gz")
    if set(manifest["archives"]) != expected:
        raise ValueError("archive list does not cover the required platforms")
    checksums = {}
    for line in (output / "checksums.txt").read_text().splitlines():
        digest, name = line.split("  ", 1)
        if name in checksums or Path(name).name != name:
            raise ValueError("repeated or unsafe checksum filename")
        checksums[name] = digest
    files = {path.name for path in output.iterdir() if path.is_file() and path.name != "checksums.txt"}
    if set(checksums) != files or not expected.issubset(files):
        raise ValueError("checksums must cover every release file exactly once")
    for name, digest in checksums.items():
        if hashlib.sha256((output / name).read_bytes()).hexdigest() != digest:
            raise ValueError("checksum mismatch: " + name)
    host_system = platform.system().lower()
    host_arch = {"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine())
    smoked = 0
    for name in sorted(expected):
        with tarfile.open(output / name) as bundle:
            members = bundle.getmembers()
            names = [member.name for member in members]
            if len(names) != len(set(names)) or any(not member.isfile() or member.name.startswith("/") or ".." in Path(member.name).parts for member in members):
                raise ValueError("archive must contain unique safe regular files")
            record = json.load(bundle.extractfile("release.json"))
            for key in ("version", "revision", "created", "dirty", "snapshot", "toolchain"):
                if record[key] != manifest[key]:
                    raise ValueError("archive source record differs: " + name)
            component = record["component"]
            common = {"LICENSE", "README.md", "release.json"}
            if component == "urth-website":
                if "website/index.html" not in names or any(member not in common and not member.startswith("website/") for member in names):
                    raise ValueError("website archive contains unexpected files")
            else:
                if set(names) != common | {component} or bundle.getmember(component).mode != 0o755:
                    raise ValueError("binary archive contains unexpected files or permissions")
                expected_name = f"{component}_{record['version']}_{record['os']}_{record['arch']}.tar.gz"
                if expected_name != name:
                    raise ValueError("archive filename differs from its platform record")
                with tempfile.TemporaryDirectory(prefix="urth-release-check-") as temporary:
                    binary = Path(temporary) / component
                    binary.write_bytes(bundle.extractfile(component).read())
                    build_info = subprocess.check_output(["go", "version", "-m", str(binary)], text=True)
                    if "vcs.revision=" + manifest["revision"] not in build_info:
                        raise ValueError("binary VCS commit differs from its archive")
                    expected_dirty = str(manifest["dirty"]).lower()
                    if "vcs.modified=" + expected_dirty not in build_info:
                        raise ValueError("binary dirty-source marker differs from its archive")
                    for setting, expected_value in (("GOOS", record["os"]), ("GOARCH", record["arch"]), ("CGO_ENABLED", "0")):
                        if "\tbuild\t" + setting + "=" + expected_value + "\n" not in build_info:
                            raise ValueError("binary platform/build setting differs: " + setting)
                    if smoke and record["os"] == host_system and record["arch"] == host_arch:
                        binary.chmod(0o755)
                        result = subprocess.run([str(binary), "--help"], capture_output=True, text=True, check=True)
                        if "Usage:" not in result.stdout:
                            raise ValueError("native help does not report usage")
                        smoked += 1
    if smoke and smoked == 0:
        raise ValueError("no archive matches this host for a help smoke check")
    print(f"Verified {len(expected)} archives, all checksums and source records; native help checks: {smoked}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--smoke", action="store_true", help="run --help for this host's binaries; never start services")
    options = parser.parse_args()
    check(options.output, options.smoke)
