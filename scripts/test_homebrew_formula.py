"""Homebrew formula rendering; no network or tap access."""

import hashlib
import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("homebrew_formula", Path(__file__).with_name("homebrew-formula.py"))
formula = importlib.util.module_from_spec(spec)
spec.loader.exec_module(formula)


class HomebrewFormulaTest(unittest.TestCase):
    def setUp(self):
        self.dir = Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.archives = self.dir / "archives"
        self.archives.mkdir()
        self.output = self.dir / "tap" / "Formula" / f"{formula.BINARY}.rb"

    def publish(self, tag):
        digests = {}
        for _, _, os_name, arch in formula.PLATFORMS:
            name = formula.archive_name(tag, os_name, arch)
            body = f"{name} contents".encode()
            (self.archives / name).write_bytes(body)
            digests[name] = hashlib.sha256(body).hexdigest()
        return digests

    def run_main(self, tag):
        return formula.main(["--tag", tag, "--archives", str(self.archives), "--output", str(self.output)])

    def test_each_platform_gets_its_own_archive_and_digest(self):
        digests = self.publish("v1.2.3")
        self.assertEqual(self.run_main("v1.2.3"), 0)
        text = self.output.read_text()
        self.assertIn('version "1.2.3"', text)
        for name, digest in digests.items():
            url = f"https://github.com/{formula.REPOSITORY}/releases/download/v1.2.3/{name}"
            # Each URL is followed by its own digest, so no platform can install
            # another platform's archive.
            self.assertEqual(text.count(url), 1, url)
            self.assertIn(f'url "{url}"\n      sha256 "{digest}"', text)
        self.assertEqual(text.count("sha256 "), 4)

    def test_archives_land_in_the_matching_platform_block(self):
        self.publish("v1.2.3")
        self.run_main("v1.2.3")
        text = self.output.read_text()
        macos, linux = text.split("on_linux do")
        self.assertNotIn("_linux_", macos)
        self.assertNotIn("_darwin_", linux)
        for block in (macos, linux.split("def install")[0]):
            arm, intel = block.split("on_intel do")
            self.assertIn("_arm64.tar.gz", arm)
            self.assertNotIn("_amd64.tar.gz", arm)
            self.assertIn("_amd64.tar.gz", intel)

    def test_prereleases_and_malformed_tags_are_refused(self):
        for tag in ("v0.1.0-rc.1", "0.1.0", "v01.2.3", "v1.2", "v1.2.3+build"):
            with self.subTest(tag=tag):
                self.publish(tag)
                self.assertEqual(self.run_main(tag), 1)
                self.assertFalse(self.output.exists())

    def test_missing_archive_is_refused(self):
        self.publish("v1.2.3")
        (self.archives / formula.archive_name("v1.2.3", "linux", "arm64")).unlink()
        self.assertEqual(self.run_main("v1.2.3"), 1)
        self.assertFalse(self.output.exists())

    def test_an_older_release_never_replaces_a_newer_formula(self):
        for tag in ("v1.10.0", "v1.9.0"):
            self.publish(tag)
        self.assertEqual(self.run_main("v1.10.0"), 0)
        rendered = self.output.read_text()
        # Numeric, not lexical: 1.9.0 sorts after 1.10.0 as text.
        self.assertEqual(self.run_main("v1.9.0"), 1)
        self.assertEqual(self.output.read_text(), rendered)

    def test_rerendering_the_same_release_is_identical(self):
        self.publish("v1.2.3")
        self.run_main("v1.2.3")
        first = self.output.read_text()
        self.assertEqual(self.run_main("v1.2.3"), 0)
        self.assertEqual(self.output.read_text(), first)

    def test_a_formula_without_a_version_is_not_overwritten(self):
        self.publish("v1.2.3")
        self.output.parent.mkdir(parents=True)
        self.output.write_text("class Hand < Formula\nend\n")
        self.assertEqual(self.run_main("v1.2.3"), 1)
        self.assertEqual(self.output.read_text(), "class Hand < Formula\nend\n")


if __name__ == "__main__":
    unittest.main()
