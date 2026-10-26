"""Local release safety regressions; never connects to ECS."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).parent / filename)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


remote = load("remote_release", "remote-release.py")
local = load("live_release", "live-release.py")


class SafetyTests(unittest.TestCase):
    def test_env_replacement_uses_compose_variable_names(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            release = root / "release"
            release.mkdir()
            (root / "production.env").write_text("SUB2API_IMAGE=old-main\nSUPPLIER_IMAGE=old-supplier\nKEEP=fixture\n")
            manifest = {"components": [{"name": "main"}, {"name": "supplier"}], "deployment": {"image_tags": {"main": "new-main", "supplier": "new-supplier"}}, "source": {"commit": "fixture"}}
            calls = []
            def fake_run(command, **kwargs):
                calls.append(command)
                info = [{"Config": {"Labels": {"org.opencontainers.image.revision": "fixture"}}}]
                return subprocess.CompletedProcess(command, 0, json.dumps(info), "")
            with patch.object(remote, "run", side_effect=fake_run), patch.object(remote, "wait_healthy"), patch.object(remote, "local_health", return_value=200):
                remote.activate(root, release, manifest, root, False)
            self.assertEqual((root / "production.env").read_text(), "SUB2API_IMAGE=new-main\nSUPPLIER_IMAGE=new-supplier\nKEEP=fixture\n")
            self.assertTrue(any(command[-2:] == ["sub2api", "supplier"] for command in calls))

    def test_inspect_does_not_log_runtime_credentials(self):
        remote.COMMAND_LOG.clear()
        remote.record(["docker", "inspect", "sub2api"], subprocess.CompletedProcess([], 0, "SECRET_FIXTURE", ""))
        self.assertNotIn("SECRET_FIXTURE", json.dumps(remote.COMMAND_LOG))

    def test_run_supports_cwd_and_binary_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            with (Path(tmp) / "output").open("wb") as output:
                result = remote.run([sys.executable, "-c", "print('fixture')"], cwd=tmp, stdout=output)
            self.assertEqual(result.returncode, 0)
            self.assertEqual((Path(tmp) / "output").read_text().strip(), "fixture")

    def test_main_only_keeps_supplier_network_overlay(self):
        command = remote.compose_args(Path("/root"), Path("/release"), Path("/env"), {"main"})
        self.assertIn(str(Path("/release") / "compose.supplier.yml"), command)
        self.assertEqual(remote.service_names({"main"}), ["sub2api"])

    def test_dirty_status_preserves_leading_status_column(self):
        with patch.object(local, "run", return_value=subprocess.CompletedProcess([], 0, " M ops/deployment/DEPLOYMENT.md\n", "")):
            self.assertEqual(local.dirty_paths(), [Path("ops/deployment/DEPLOYMENT.md")])

    def test_invalid_components_fail_closed(self):
        with self.assertRaises(ValueError):
            local.parse_components("main,unknown")


if __name__ == "__main__":
    unittest.main()
