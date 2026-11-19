import hashlib
import importlib.util
import io
import json
import contextlib
import os
import tempfile
from unittest.mock import patch
from pathlib import Path
import unittest
import zipfile

spec = importlib.util.spec_from_file_location("upgrade092", Path(__file__).parents[1] / "upgrade_v0.9.2.py")
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

def archive(extra=None):
    files = {"foxray_v0.9.2": b"\x7fELFfixture", "web/index.html": b"<html>test</html>",
             "rules/gfwlist_domains.json": b'{"direct":[],"proxy":["example.test"]}'}
    files.update(extra or {})
    manifest = {k: hashlib.sha256(v).hexdigest() for k, v in files.items()}
    stream = io.BytesIO()
    with zipfile.ZipFile(stream, "w") as z:
        for name, value in files.items(): z.writestr(name, value)
        z.writestr("MANIFEST_SHA256.json", json.dumps(manifest))
    raw = stream.getvalue()
    return raw, hashlib.sha256(raw).hexdigest()

class UpgradeArchiveTest(unittest.TestCase):
    def test_new_display_default_does_not_mask_real_changes(self):
        state = {"manager_settings": {"presentation": {"content_width_percent": 80}}}
        def response(base, path):
            if path == "/api/state": return state
            return {"complete": True}
        with patch.object(m, "get_json", side_effect=response):
            before = m.snapshot("http://localhost/api/health")
            self.assertNotIn("home_header_auto_hide", state["manager_settings"]["presentation"])
            state["manager_settings"]["presentation"]["home_header_auto_hide"] = False
            self.assertEqual(before, m.snapshot("http://localhost/api/health"))
            state["manager_settings"]["presentation"]["home_header_auto_hide"] = True
            self.assertNotEqual(before["configuration"], m.snapshot("http://localhost/api/health")["configuration"])
    def test_public_rules_are_seeded_without_overwriting_existing_data(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            files = {"rules/gfwlist_domains.json": b'{"direct":[],"proxy":["example.test"]}'}
            m.ensure_public_rules(base, files)
            target = base / "rules/gfwlist_domains.json"
            self.assertEqual(target.read_bytes(), files["rules/gfwlist_domains.json"])
            target.write_bytes(b"existing dataset")
            m.ensure_public_rules(base, files)
            self.assertEqual(target.read_bytes(), b"existing dataset")
    def test_valid_package(self):
        raw, sha = archive()
        self.assertIn("web/index.html", m.verify_archive(raw, sha))
    def test_wrong_digest(self):
        raw, _ = archive()
        with self.assertRaises(ValueError): m.verify_archive(raw, "0" * 64)
    def test_traversal(self):
        raw, sha = archive({"../escape": b"bad"})
        with self.assertRaises(ValueError): m.verify_archive(raw, sha)
    def test_missing_manifest_entry(self):
        raw, _ = archive()
        stream = io.BytesIO()
        with zipfile.ZipFile(io.BytesIO(raw)) as src, zipfile.ZipFile(stream,"w") as dst:
            for name in src.namelist(): dst.writestr(name, b"{}" if name == "MANIFEST_SHA256.json" else src.read(name))
        raw = stream.getvalue()
        with self.assertRaises(ValueError): m.verify_archive(raw,hashlib.sha256(raw).hexdigest())

@unittest.skipIf(os.name == "nt", "Linux symlink deployment test")
class UpgradeSwitchTest(unittest.TestCase):
    def scenario(self, fail):
        from pathlib import Path
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            old = base / "releases/old"
            (old / "web").mkdir(parents=True)
            binary = old / "foxray_previous"
            binary.write_bytes(b"old")
            (base / "foxray").symlink_to(binary)
            (base / "web").symlink_to(old / "web")
            raw, sha = archive()
            package = base / "package.zip"
            package.write_bytes(raw)
            args = ["upgrade", "--base", str(base), "--archive", str(package), "--sha256", sha, "--apply"]
            baseline = {"configuration": "same", "core_pid": 123}
            with patch("sys.argv", args), patch.object(m, "get_json", return_value={"version":"0.9.0"}), \
                 patch.object(m, "snapshot", return_value=baseline), patch.object(m, "command", return_value=""), \
                 patch.object(m, "healthy", side_effect=[False,True] if fail else [True]), \
                 patch.object(m.os, "geteuid", return_value=0), contextlib.redirect_stdout(io.StringIO()):
                if fail:
                    with self.assertRaises(RuntimeError): m.main()
                    self.assertEqual((base / "foxray").resolve(), binary)
                    self.assertEqual((base / "web").resolve(), old / "web")
                else:
                    m.main()
                    self.assertEqual((base / "foxray").resolve().name, "foxray_v0.9.2")
                    self.assertTrue(list((base / "private").glob("migration*.json")))
    def test_success_switch(self): self.scenario(False)
    def test_health_failure_rolls_back(self): self.scenario(True)

if __name__ == "__main__": unittest.main()
