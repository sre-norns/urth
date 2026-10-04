"""Release protocol and archive regressions; no Git refs or network writes."""

import importlib.util
import json
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("build_release", Path(__file__).with_name("build-release.py"))
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)
spec = importlib.util.spec_from_file_location("record_images", Path(__file__).with_name("record-release-images.py"))
images = importlib.util.module_from_spec(spec)
spec.loader.exec_module(images)


class ReleaseProtocolTest(unittest.TestCase):
    def source(self, version, dirty=False, tag_matches=True):
        commit = "a" * 40

        def fake_git(*args):
            if args == ("rev-parse", "HEAD"):
                return commit
            if args[0] == "show":
                return "1700000000"
            if args[0] == "status":
                return " M example" if dirty else ""
            if args[0] == "rev-parse":
                return commit if tag_matches else "b" * 40
            raise AssertionError(args)

        with patch.object(builder, "git", fake_git):
            return builder.metadata(version)

    def test_prerelease_cannot_select_stable_alias(self):
        for version in ("v0.1.0-rc.1", "v2.0.0-alpha", "v2.0.0-0", "v2.0.0-01a"):
            with self.subTest(version=version):
                entry = self.source(version)
                self.assertFalse(entry["stable"])
                self.assertFalse(entry["snapshot"])
        self.assertTrue(self.source("v0.1.0")["stable"])
        self.assertFalse(self.source(None)["stable"])

    def test_version_rejects_malformed_tags(self):
        for version in ("v01.0.0", "v1.0", "v1.0.0-01", "v1.0.0-", "v1.0.0+build", "../v1.0.0", "v1.0.0\n", "v1.0.0-" + "a" * 128):
            with self.subTest(version=version), self.assertRaises(ValueError):
                self.source(version)

    def test_version_requires_clean_matching_tag(self):
        with self.assertRaisesRegex(ValueError, "clean worktree"):
            self.source("v0.1.0", dirty=True)
        with self.assertRaisesRegex(ValueError, "tag must name HEAD"):
            self.source("v0.1.0", tag_matches=False)
        self.assertTrue(self.source(None, dirty=True)["dirty"])

    def test_archive_has_only_explicit_entries_and_fixed_metadata(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "private.npmrc").write_text("private sentinel")
            entries = {"tool": (b"executable", 0o755), "release.json": (b"{}\n", 0o644)}
            builder.archive(root / "a.tar.gz", entries, 1700000000)
            builder.archive(root / "b.tar.gz", dict(reversed(list(entries.items()))), 1700000000)
            self.assertEqual((root / "a.tar.gz").read_bytes(), (root / "b.tar.gz").read_bytes())
            with tarfile.open(root / "a.tar.gz") as archive:
                self.assertEqual(archive.getnames(), ["release.json", "tool"])
                for member in archive.getmembers():
                    self.assertEqual((member.uid, member.gid, member.mtime), (0, 0, 1700000000))
                self.assertEqual(archive.getmember("tool").mode, 0o755)


class GoReleaserTest(unittest.TestCase):
    def command(self, snapshot, snap=True):
        calls = []
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            built = root / "dist" / "goreleaser"
            built.mkdir(parents=True)
            for name in ("tool_v1_linux_amd64.tar.gz", "tool_1_amd64.deb", "tool_1_amd64.snap", "config.yaml", "metadata.json"):
                (built / name).write_text(name)
            with patch.object(builder, "ROOT", root), patch.object(builder.subprocess, "run", lambda command, **options: calls.append((command, options["env"]))):
                found = builder.goreleaser(dict(snapshot=snapshot, version="v1"), root / "records", snap)
        self.assertEqual([path.name for path in found], ["tool_1_amd64.deb", "tool_1_amd64.snap", "tool_v1_linux_amd64.tar.gz"])
        (command, env), = calls
        self.assertEqual((env["RELEASE_VERSION"], env["GOWORK"]), ("v1", "off"))
        return command[1:]

    def test_snapshot_and_version_builds_never_publish(self):
        self.assertEqual(self.command(snapshot=True), ["release", "--clean", "--snapshot"])
        self.assertEqual(self.command(snapshot=False), ["release", "--clean", "--skip=publish,announce"])
        self.assertEqual(self.command(snapshot=True, snap=False), ["release", "--clean", "--snapshot", "--skip=snapcraft"])
        self.assertEqual(self.command(snapshot=False, snap=False), ["release", "--clean", "--skip=publish,announce,snapcraft"])


class ImageRecordTest(unittest.TestCase):
    def entries(self):
        return [dict(image=image, version="v0.1.0", revision="a" * 40, digest="sha256:" + "b" * 64) for image in sorted(images.IMAGES)]

    def record(self, records):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output, incoming = root / "release", root / "digests"
            output.mkdir()
            incoming.mkdir()
            (output / "release-manifest.json").write_text(json.dumps(dict(version="v0.1.0", revision="a" * 40)))
            for index, entry in enumerate(records):
                directory = incoming / str(index)
                directory.mkdir()
                (directory / "image.json").write_text(json.dumps(entry))
            images.record(output, incoming)
            result = json.loads((output / "images.json").read_text())
            self.assertIn("images.json", (output / "checksums.txt").read_text())
            return result

    def test_complete_image_set_matches_source(self):
        self.assertEqual(self.record(self.entries())["images"], self.entries())

    def test_wrong_missing_and_duplicate_image_are_refused(self):
        records = self.entries()
        for candidate in (records[:-1], records + [records[0]], [dict(records[0], image="urth-worker"), *records[1:]], [dict(records[0], image="ghcr.io/other/urth-worker"), *records[1:]]):
            with self.subTest(candidate=candidate), self.assertRaises(ValueError):
                self.record(candidate)

    def test_wrong_version_source_or_digest_are_refused(self):
        records = self.entries()
        for changes in (dict(version="v0.2.0"), dict(revision="c" * 40), dict(digest="sha256:short")):
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                self.record([dict(records[0], **changes), *records[1:]])


if __name__ == "__main__":
    unittest.main()
