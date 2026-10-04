#!/usr/bin/env python3
"""Verify the four image digest records and add them to release checksums."""

import argparse
import json
from pathlib import Path
import re
import sys

from importlib.util import module_from_spec, spec_from_file_location

sys.dont_write_bytecode = True
spec = spec_from_file_location("build_release", Path(__file__).with_name("build-release.py"))
builder = module_from_spec(spec)
spec.loader.exec_module(builder)

IMAGES = {"ghcr.io/sre-norns/" + name for name in ("urth-api-srv", "urth-worker", "urthctl", "urth-web")}


def record(output, images):
    manifest = json.loads((output / "release-manifest.json").read_text())
    records = []
    names = set()
    for file in sorted(images.rglob("*.json")):
        entry = json.loads(file.read_text())
        name = entry["image"]
        if name not in IMAGES or name in names:
            raise ValueError("unexpected or repeated image digest record")
        if entry["version"] != manifest["version"] or entry["revision"] != manifest["revision"]:
            raise ValueError("image version/source differs from archive source")
        if not re.fullmatch(r"sha256:[0-9a-f]{64}", entry["digest"]):
            raise ValueError("invalid image digest")
        names.add(name)
        records.append(entry)
    if names != IMAGES:
        raise ValueError("all four image digest records are required")
    (output / "images.json").write_bytes(builder.encoded({"images": records}))
    builder.checksums(output)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--images", type=Path, required=True)
    args = parser.parse_args()
    try:
        record(args.output, args.images)
    except ValueError as error:
        parser.error(str(error))


if __name__ == "__main__":
    main()
