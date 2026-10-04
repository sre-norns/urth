#!/usr/bin/env python3
"""Inspect release source records, checksums, archive and package members and native help."""

import argparse
import hashlib
import io
import json
from pathlib import Path
import platform
import subprocess
import tarfile
import tempfile

# Debian package -> (installed executable, command on PATH); snap -> executable.
DEBS = {
    "urth-api-server": ("usr/libexec/urth/api-server", "usr/bin/urth-api-server"),
    "urth-worker": ("usr/libexec/urth/nats-worker", "usr/bin/urth-worker"),
    "urthctl": ("usr/bin/urthctl", "usr/bin/urthctl"),
}
SNAPS = {"urth-api-server": "api-server", "urthctl": "urthctl"}
ARCHES = ("amd64", "arm64")


def verify_binary(data, manifest, system, arch):
    with tempfile.TemporaryDirectory(prefix="urth-release-check-") as temporary:
        binary = Path(temporary) / "binary"
        binary.write_bytes(data)
        build_info = subprocess.check_output(["go", "version", "-m", str(binary)], text=True)
    if "vcs.revision=" + manifest["revision"] not in build_info:
        raise ValueError("binary VCS commit differs from its release")
    if "vcs.modified=" + str(manifest["dirty"]).lower() not in build_info:
        raise ValueError("binary dirty-source marker differs from its release")
    for setting, expected_value in (("GOOS", system), ("GOARCH", arch), ("CGO_ENABLED", "0")):
        if "\tbuild\t" + setting + "=" + expected_value + "\n" not in build_info:
            raise ValueError("binary platform/build setting differs: " + setting)


def package(manifest, prefix, arch, suffix):
    found = [name for name in manifest["packages"] if name.startswith(prefix + "_") and name.endswith(f"_{arch}{suffix}")]
    if len(found) != 1:
        raise ValueError(f"expected one {prefix} {arch} {suffix} package")
    return found[0]


def check_packages(output, manifest):
    expected = set()
    for name, (executable, command) in DEBS.items():
        for arch in ARCHES:
            deb = package(manifest, name, arch, ".deb")
            expected.add(deb)
            control = subprocess.check_output(["dpkg-deb", "--field", str(output / deb), "Package", "Architecture"], text=True)
            if control.split() != ["Package:", name, "Architecture:", arch]:
                raise ValueError("Debian control record differs: " + deb)
            tree = subprocess.run(["dpkg-deb", "--fsys-tarfile", str(output / deb)], capture_output=True, check=True).stdout
            with tarfile.open(fileobj=io.BytesIO(tree)) as bundle:
                members = {member.name.removeprefix("./"): member for member in bundle.getmembers()}
                if command != executable and members[command].linkname != "/" + executable:
                    raise ValueError("Debian command link differs: " + deb)
                if members[executable].mode != 0o755:
                    raise ValueError("Debian executable permissions differ: " + deb)
                verify_binary(bundle.extractfile(members[executable]).read(), manifest, "linux", arch)
    if manifest["snap"]:
        for name, executable in SNAPS.items():
            for arch in ARCHES:
                snap = package(manifest, name, arch, ".snap")
                expected.add(snap)
                with tempfile.TemporaryDirectory(prefix="urth-release-snap-") as temporary:
                    root = Path(temporary) / "snap"
                    subprocess.run(["unsquashfs", "-q", "-n", "-d", str(root), str(output / snap), "meta/snap.yaml", executable], check=True, stdout=subprocess.DEVNULL)
                    record = (root / "meta" / "snap.yaml").read_text()
                    if f"name: {name}\n" not in record or f"- {arch}\n" not in record:
                        raise ValueError("snap metadata differs: " + snap)
                    verify_binary((root / executable).read_bytes(), manifest, "linux", arch)
    if set(manifest["packages"]) != expected:
        raise ValueError("package list does not match the required packages")
    return len(expected)


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
    if set(checksums) != files or not expected.issubset(files) or not set(manifest["packages"]).issubset(files):
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
                data = bundle.extractfile(component).read()
                verify_binary(data, manifest, record["os"], record["arch"])
                if smoke and record["os"] == host_system and record["arch"] == host_arch:
                    with tempfile.TemporaryDirectory(prefix="urth-release-check-") as temporary:
                        binary = Path(temporary) / component
                        binary.write_bytes(data)
                        binary.chmod(0o755)
                        result = subprocess.run([str(binary), "--help"], capture_output=True, text=True, check=True)
                    if "Usage:" not in result.stdout:
                        raise ValueError("native help does not report usage")
                    smoked += 1
    if smoke and smoked == 0:
        raise ValueError("no archive matches this host for a help smoke check")
    packages = check_packages(output, manifest)
    print(f"Verified {len(expected)} archives, {packages} packages, all checksums and source records; native help checks: {smoked}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--smoke", action="store_true", help="run --help for this host's binaries; never start services")
    options = parser.parse_args()
    check(options.output, options.smoke)
