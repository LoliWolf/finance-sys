"""Small byte limits exercise the production policy without generating GB logs."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from scripts.service_runtime import SegmentedLogs, supervise
import start_prod as prod


class RuntimeTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="finance-runtime-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()

    def test_segments_and_high_water_retain_only_latest_segment(self):
        logs = SegmentedLogs(self.root, segment_bytes=10, high_water_bytes=100)
        logs.write(b"a" * 90)
        self.assertEqual(len(logs._segments()), 9)
        logs.write(b"latest!!!!")
        self.assertEqual(len(logs._segments()), 1)
        self.assertEqual(logs._segments()[0].read_bytes(), b"latest!!!!")
        logs.write(b"next")
        self.assertEqual(len(logs._segments()), 2)
        self.assertTrue(all(p.stat().st_size <= 10 for p in logs._segments()))

    def test_large_single_write_is_split_and_old_files_are_pruned(self):
        logs = SegmentedLogs(self.root, 10, 100)
        logs.write(b"a" * 1000 + b"END")
        self.assertEqual(sum(p.stat().st_size for p in logs._segments()), 13)
        self.assertTrue(logs._segments()[-1].read_bytes().endswith(b"END"))
        self.assertTrue(all(p.stat().st_size <= 10 for p in logs._segments()))
        reopened = SegmentedLogs(self.root, 10, 100)
        reopened.write(b"more")
        self.assertEqual(sum(p.stat().st_size for p in reopened._segments()), 17)

    @unittest.skipIf(os.name == "nt", "creating symlinks requires extra Windows privileges")
    def test_rotation_never_deletes_unrelated_files_or_follows_symlinks(self):
        sentinel = self.root / "other.log"
        sentinel.write_bytes(b"keep")
        logs = SegmentedLogs(self.root, 10, 100)
        logs.write(b"x" * 200)
        self.assertEqual(sentinel.read_bytes(), b"keep")
        (self.root / "00000000000000000021.log").symlink_to(sentinel)
        with self.assertRaises(ValueError):
            logs.write(b"not appended")
        self.assertEqual(sentinel.read_bytes(), b"keep")

    def test_independent_processes_share_one_collection(self):
        script = ("from scripts.service_runtime import SegmentedLogs; import sys; "
                  "logs=SegmentedLogs(sys.argv[1],100,1000); "
                  "[logs.write(b'x'*37) for _ in range(70)]")
        children = [subprocess.Popen([sys.executable, "-B", "-c", script, str(self.root)],
                                    cwd=Path(__file__).resolve().parents[2]) for _ in range(3)]
        for child in children:
            self.assertEqual(child.wait(timeout=15), 0)
        segments = SegmentedLogs(self.root, 100, 1000)._segments()
        self.assertTrue(segments)
        self.assertTrue(all(p.stat().st_size <= 100 for p in segments))
        self.assertLess(sum(p.stat().st_size for p in segments), 1000)

    def test_supervisor_captures_both_streams_and_cleans_workspace_on_failure(self):
        stale = self.root / "work/api"
        stale.mkdir(parents=True)
        (stale / "old.pdf").write_bytes(b"old")
        code = ("import os,sys,pathlib; "
                "assert not (pathlib.Path(os.environ['TMPDIR'])/'old.pdf').exists(); "
                "(pathlib.Path(os.environ['TMPDIR'])/'new.png').write_bytes(b'image'); "
                "print('output');print('error',file=sys.stderr);sys.exit(7)")
        self.assertEqual(supervise([sys.executable, "-c", code], self.root, self.root, "api", os.environ), 7)
        self.assertFalse(stale.exists())
        self.assertFalse((self.root / "api.child.json").exists())
        content = b"".join(p.read_bytes() for p in SegmentedLogs(self.root / "logs/runtime")._segments())
        self.assertIn(b"output", content)
        self.assertIn(b"error", content)

    def test_supervisor_refuses_live_previous_child_before_cleaning(self):
        workspace = self.root / "work/api"
        workspace.mkdir(parents=True)
        (workspace / "active.pdf").write_bytes(b"keep")
        (self.root / "api.child.json").write_text(json.dumps({"pid": os.getpid()}))
        with self.assertRaisesRegex(RuntimeError, "still alive"):
            supervise([sys.executable, "-c", "pass"], self.root, self.root, "api", os.environ)
        self.assertEqual((workspace / "active.pdf").read_bytes(), b"keep")

    def test_runtime_mode_requires_production_and_cannot_be_used_as_build(self):
        with patch.object(prod, "ROOT", self.root), patch.object(prod, "report_runtime_failure") as report, \
                patch.dict(os.environ, {"FINANCE_SYS_ENV": "TEST"}):
            with self.assertRaises(SystemExit) as result:
                prod.main(["--supervise", "agent"])
        self.assertEqual(result.exception.code, 1)
        report.assert_called_once()
        with self.assertRaises(SystemExit):
            prod.main(["--build-only", "--supervise", "api"])

    @unittest.skipIf(os.name == "nt", "POSIX signal shutdown")
    def test_supervisor_forwards_shutdown_and_cleans_after_child_exit(self):
        script = ("import os,sys;from scripts.service_runtime import supervise;"
                  "code=\"import os,pathlib,time;pathlib.Path(os.environ['TMPDIR'],'working.png').write_bytes(b'x');time.sleep(60)\";"
                  "sys.exit(supervise([sys.executable,'-c',code],sys.argv[1],sys.argv[1],'api',os.environ))")
        child = subprocess.Popen([sys.executable, "-B", "-c", script, str(self.root)],
                                 cwd=Path(__file__).resolve().parents[2])
        self.addCleanup(lambda: child.kill() if child.poll() is None else None)
        import time
        deadline = time.monotonic() + 5
        while not (self.root / "work/api/working.png").exists():
            self.assertLess(time.monotonic(), deadline)
            time.sleep(.05)
        child.terminate()
        self.assertEqual(child.wait(timeout=10), 0)
        self.assertFalse((self.root / "work/api").exists())
        self.assertFalse((self.root / "api.child.json").exists())

    def test_trading_agent_is_not_started_when_disabled_or_already_healthy(self):
        cfg = {"trading": {"agent": {"enabled": False}}}
        with patch.object(prod, "launch") as launch:
            prod.ensure_trading_agent(cfg, self.root, {}, self.root, "Darwin")
        launch.assert_not_called()
        cfg["trading"]["agent"] = {"enabled": True, "health_endpoint": "http://127.0.0.1:8110/healthz"}
        with patch.object(prod, "healthy", return_value=True), patch.object(prod, "launch") as launch:
            prod.ensure_trading_agent(cfg, self.root, {}, self.root, "Darwin")
        launch.assert_not_called()

    def test_supervised_job_identity_resolves_the_managed_child(self):
        binary = self.root / "tmp/production/releases/v1/finance-sys-api"
        raw = ("program = %s\narguments = {\n%s\n%s\n"
               "--supervise\napi\n--binary\n%s\n}\npid = 123\n" %
               (sys.executable, sys.executable, self.root / "start_prod.py", binary))
        with patch.object(prod, "ROOT", self.root), patch.object(prod, "tool", side_effect=lambda s: s), \
                patch.object(prod, "run", return_value=subprocess.CompletedProcess([], 0, raw)):
            info = prod.loaded_job_info(prod.API_LABEL)
        self.assertEqual(info["binary"], binary)
        self.assertTrue(info["supervised"])
        with patch.object(prod, "ROOT", self.root / "other"), patch.object(prod, "tool", side_effect=lambda s: s), \
                patch.object(prod, "run", return_value=subprocess.CompletedProcess([], 0, raw)):
            with self.assertRaisesRegex(prod.DeploymentError, "Unverified"):
                prod.loaded_job_info(prod.API_LABEL)


if __name__ == "__main__":
    unittest.main()
