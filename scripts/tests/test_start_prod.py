"""Production launcher safety checks; no live services or middleware are used."""

import json
import os
from pathlib import Path
import plistlib
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import start_prod as prod


def config():
    return {"meta": {"config_version": 44},
            "service": {"http": {"host": "0.0.0.0", "port": 30006, "port_test": 30005}},
            "database": {"driver": "mysql", "dsn": "user:private-value@tcp(127.0.0.1:3306)/expert_trade?parseTime=true"},
            "agent": {"endpoint": "http://127.0.0.1:8108/v1/resolve-document"}}


class ProductionLauncherTests(unittest.TestCase):
    def setUp(self):
        quiet = patch.object(prod, "info")
        quiet.start()
        self.addCleanup(quiet.stop)
        self.temporary = tempfile.TemporaryDirectory(prefix="finance-prod-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.env_file = self.root / "bootstrap_go122.env.example"
        self.env_file.write_text("NACOS_SERVER_ADDR=127.0.0.1:8848\n", encoding="utf-8")

    def test_environment_address_precedes_example(self):
        self.assertEqual(prod.nacos_address(self.root, environ={"NACOS_SERVER_ADDR": "other:8848"}), "other:8848")
        self.assertEqual(prod.nacos_address(self.root, environ={}), "127.0.0.1:8848")

    def test_explicit_file_precedes_environment_and_accepts_bom(self):
        self.env_file.write_text("# comment\nNACOS_SERVER_ADDR=\"other:8848\"\n", encoding="utf-8-sig")
        self.assertEqual(prod.nacos_address(self.root, self.env_file, {"NACOS_SERVER_ADDR": "wrong:1"}), "other:8848")

    def test_bootstrap_rejects_business_config_duplicate_and_empty(self):
        for raw in ("FINANCE_SYS_ENV=PROD", "NACOS_SERVER_ADDR=", "NACOS_SERVER_ADDR=a:1\nNACOS_SERVER_ADDR=b:2", "unexpected"):
            with self.subTest(raw=raw):
                self.env_file.write_text(raw)
                with self.assertRaises(prod.DeploymentError):
                    prod.nacos_address(self.root, environ={})

    def test_config_requires_production_schema_without_leaking_dsn(self):
        cfg = config()
        self.assertIs(prod.validate_config(cfg), cfg)
        cfg["database"]["dsn"] = cfg["database"]["dsn"].replace("expert_trade?", "expert_trade_test?")
        with self.assertRaises(prod.DeploymentError) as error:
            prod.validate_config(cfg)
        self.assertNotIn("private-value", str(error.exception))

    def test_config_rejects_shared_invalid_ports_and_credential_url(self):
        for value in (True, 0, 65536, "30006", 30005):
            with self.subTest(value=value):
                cfg = config()
                cfg["service"]["http"]["port"] = value
                with self.assertRaises(prod.DeploymentError):
                    prod.validate_config(cfg)
        cfg = config()
        cfg["agent"]["endpoint"] = "http://user:secret@127.0.0.1:8108"
        with self.assertRaises(prod.DeploymentError):
            prod.validate_config(cfg)

    def test_test_environment_cannot_inherit_real_db_opt_ins(self):
        source = {"FINANCE_SYS_ENV": "PROD", "FINANCE_SYS_M0_NACOS_DML_ACK": "write-real-db",
                  "FINANCE_SYS_RUN_REAL_DOCUMENT_REPORT": "1", "NACOS_SERVER_ADDR": "prod:8848",
                  "NACOS_PASSWORD": "secret", "VITE_API_BASE": "http://wrong", "GOOS": "windows"}
        env = prod.clean_environment(source)
        self.assertEqual(env["FINANCE_SYS_ENV"], "TEST")
        self.assertEqual(env["GOTOOLCHAIN"], "local")
        for name in source:
            if name != "FINANCE_SYS_ENV":
                self.assertNotIn(name, env)
        self.assertEqual(source["FINANCE_SYS_ENV"], "PROD")
        env = prod.clean_environment(source, production=True, address="correct:8848")
        self.assertEqual(env["FINANCE_SYS_ENV"], "PROD")
        self.assertEqual(env["NACOS_SERVER_ADDR"], "correct:8848")

    def test_signing_requires_uuid_and_named_adhoc_identity(self):
        uuid = "12345678-1234-1234-1234-123456789ABC"
        def outputs(argv, **kwargs):
            text = ""
            if "-dv" in argv:
                text = "Identifier=" + prod.SIGNING_IDENTIFIER + "\nSignature=adhoc\n"
            if "-l" in argv:
                text = "cmd LC_UUID\ncmdsize 24\nuuid " + uuid
            return subprocess.CompletedProcess(argv, 0, text)
        with patch.object(prod, "tool", side_effect=lambda name: name), patch.object(prod, "run", side_effect=outputs) as command:
            self.assertEqual(prod.sign_macos(self.root / "api"), uuid)
            self.assertIn("--timestamp=none", command.call_args_list[0].args[0])
        with patch.object(prod, "tool", side_effect=lambda name: name), patch.object(prod, "run", return_value=subprocess.CompletedProcess([], 0, "Identifier=a.out\nSignature=adhoc\n")):
            with self.assertRaisesRegex(prod.DeploymentError, "identity"):
                prod.sign_macos(self.root / "api")
        def missing_uuid(argv, **kwargs):
            result = outputs(argv, **kwargs)
            if "-l" in argv:
                result.stdout = "cmd LC_BUILD_VERSION"
            return result
        with patch.object(prod, "tool", side_effect=lambda name: name), patch.object(prod, "run", side_effect=missing_uuid):
            with self.assertRaisesRegex(prod.DeploymentError, "LC_UUID"):
                prod.sign_macos(self.root / "api")

    def test_build_only_never_contacts_nacos_or_deploys(self):
        with patch.object(prod, "ROOT", self.root), patch.object(prod.platform, "system", return_value="Darwin"), \
                patch.object(prod, "build_release", return_value=self.root / "api") as build, \
                patch.object(prod, "load_nacos") as nacos, patch.object(prod, "deploy") as deploy:
            prod.main(["--build-only"])
            build.assert_called_once_with(self.root, "Darwin")
            nacos.assert_not_called()
            deploy.assert_not_called()

    def test_configuration_target_change_during_build_aborts_before_deployment(self):
        initial = config()
        changed = config()
        changed["service"]["http"]["port"] = 30008
        with patch.object(prod, "ROOT", self.root), patch.object(prod.platform, "system", return_value="Windows"), \
                patch.object(prod, "nacos_address", return_value="nacos:8848"), \
                patch.object(prod, "build_release", return_value=self.root / "api"), \
                patch.object(prod, "listener", return_value=None), \
                patch.object(prod, "load_nacos", side_effect=[initial, changed]), patch.object(prod, "deploy") as deploy:
            with self.assertRaisesRegex(prod.DeploymentError, "changed during the build"):
                prod.main([])
            deploy.assert_not_called()

    def test_failed_build_does_not_enter_deployment(self):
        with patch.object(prod, "ROOT", self.root), patch.object(prod.platform, "system", return_value="Windows"), \
                patch.object(prod, "nacos_address", return_value="nacos:8848"), \
                patch.object(prod, "build_release", side_effect=prod.DeploymentError("test gate failed")), \
                patch.object(prod, "listener", return_value=None), \
                patch.object(prod, "load_nacos", return_value=config()), patch.object(prod, "deploy") as deploy:
            with self.assertRaisesRegex(prod.DeploymentError, "test gate failed"):
                prod.main([])
            deploy.assert_not_called()

    def test_mac_build_generates_uuid_after_frontend_build(self):
        calls = []
        def fake_run(argv, **kwargs):
            calls.append(argv)
            if argv[:3] == ["go", "tool", "link"]:
                return subprocess.CompletedProcess(argv, 2, "Mach-O UUID; gobuildid")
            if "-o" in argv:
                Path(argv[argv.index("-o") + 1]).write_bytes(b"compiled-api")
            return subprocess.CompletedProcess(argv, 0, "")
        with patch.object(prod, "tool", side_effect=lambda name: name), patch.object(prod, "run", side_effect=fake_run), \
                patch.object(prod, "sign_macos", return_value="a-test-uuid"):
            binary = prod.build_release(self.root, "Darwin")
        api_build = next(command for command in calls if command[:2] == ["go", "build"] and "-o" in command)
        self.assertIn("-ldflags=-B=gobuildid", api_build)
        self.assertLess(calls.index(["npm", "run", "build"]), calls.index(["go", "test", "./..."]))
        manifest = json.loads((prod.release_directory(binary) / "manifest.json").read_text())
        self.assertEqual(manifest["signature"], "adhoc")
        self.assertEqual(len(manifest["sha256"]), 64)

    def test_windows_build_has_no_macos_commands(self):
        def fake_run(argv, **kwargs):
            if "-o" in argv:
                Path(argv[argv.index("-o") + 1]).write_bytes(b"windows-api")
            return subprocess.CompletedProcess(argv, 0, "")
        with patch.object(prod, "tool", side_effect=lambda name: name) as tools, patch.object(prod, "run", side_effect=fake_run), \
                patch.object(prod, "sign_macos") as sign:
            binary = prod.build_release(self.root, "Windows")
            self.assertEqual(binary.suffix, ".exe")
            sign.assert_not_called()
            self.assertNotIn(unittest.mock.call("codesign"), tools.call_args_list)

    def test_reject_unrelated_listener_and_pid_reuse(self):
        with patch.object(prod, "tool", side_effect=lambda name: name), \
                patch.object(prod, "run", return_value=subprocess.CompletedProcess([], 0, "123\n")), \
                patch.object(prod, "process_path", return_value=Path("/some/other/api")):
            with self.assertRaisesRegex(prod.DeploymentError, "unmanaged"):
                prod.listener(30006, self.root, "Darwin")
        previous = {"pid": 123, "binary": str(self.root / "tmp/api_nacos")}
        with patch.object(prod, "process_path", return_value=Path("/some/other/api")), patch.object(prod.os, "kill") as kill:
            prod.stop_process(previous, "Darwin")
            kill.assert_not_called()

    def test_managed_binary_paths_are_narrow(self):
        self.assertTrue(prod.managed_binary(self.root / "tmp/api_nacos", self.root))
        self.assertTrue(prod.managed_binary(self.root / "tmp/production/releases/v1/finance-sys-api", self.root))
        self.assertFalse(prod.managed_binary(self.root / "tmp/api_test", self.root))
        self.assertFalse(prod.managed_binary(self.root / "tmp/other/api_nacos", self.root))
        self.assertFalse(prod.managed_binary(self.root / "tmp/production/releases/v1/sub/finance-sys-api", self.root))
        self.assertTrue(prod.managed_binary(self.root / "tmp/production/releases/v1/FinanceSys.app/Contents/MacOS/finance-sys-api", self.root))
        self.assertFalse(prod.managed_binary(self.root / "tmp/production/releases/v1/Unrelated.app/Contents/MacOS/finance-sys-api", self.root))

    def test_bundle_identity_is_associated_with_launchd_and_has_usage_description(self):
        binary = prod.prepare_macos_bundle(self.root / "release")
        bundle = prod.macos_bundle(binary)
        spec = plistlib.loads((bundle / "Contents/Info.plist").read_bytes())
        self.assertEqual(spec["CFBundleIdentifier"], prod.SIGNING_IDENTIFIER)
        self.assertEqual(spec["CFBundleExecutable"], "FinanceSysLauncher")
        self.assertTrue(spec["NSLocalNetworkUsageDescription"])
        job = prod.make_plist(prod.API_LABEL, [binary], self.root, {}, self.root / "logs")
        self.assertEqual(job["AssociatedBundleIdentifiers"], [prod.SIGNING_IDENTIFIER])
        self.assertEqual(prod.release_directory(binary), self.root / "release")

    def test_native_entry_is_used_for_launchd_but_not_listener_identity(self):
        binary = prod.prepare_macos_bundle(self.root / "release")
        launcher = binary.parent / "FinanceSysLauncher"
        launcher.write_bytes(b"launcher")
        self.assertEqual(prod.macos_entry(binary), launcher)
        self.assertEqual(prod.macos_entry(self.root / "tmp/api_nacos"), self.root / "tmp/api_nacos")

    def test_no_restart_when_agent_is_healthy(self):
        with patch.object(prod, "healthy", return_value=True), patch.object(prod, "launch") as launch:
            prod.ensure_agent(config(), self.root, {}, self.root / "logs", "Darwin")
            launch.assert_not_called()

    def test_remote_unhealthy_agent_fails_without_starting_local_copy(self):
        cfg = config()
        cfg["agent"]["endpoint"] = "http://192.0.2.1:8108/v1/resolve-document"
        with patch.object(prod, "healthy", return_value=False), patch.object(prod, "launch") as launch:
            with self.assertRaisesRegex(prod.DeploymentError, "own host"):
                prod.ensure_agent(cfg, self.root, {}, self.root / "logs", "Windows")
            launch.assert_not_called()

    def test_failure_before_cutover_does_not_stop_existing_api(self):
        with patch.object(prod, "listener", return_value=None), patch.object(prod, "ensure_agent", side_effect=prod.DeploymentError("agent failed")), \
                patch.object(prod, "stop_api_jobs") as jobs, patch.object(prod, "launch") as launch:
            with self.assertRaises(prod.DeploymentError):
                prod.deploy(self.root, self.root / "release/api", config(), "nacos:8848", "Darwin")
            jobs.assert_not_called()
            launch.assert_not_called()

    def test_successful_cutover_records_only_the_verified_new_binary(self):
        binary = self.root / "tmp/production/releases/new/finance-sys-api"
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b"new")
        previous = {"pid": 123, "binary": str(self.root / "tmp/api_nacos")}
        current = {"pid": 456, "binary": str(binary)}
        with patch.object(prod, "listener", side_effect=[previous, None, current]) as listeners, \
                patch.object(prod, "ensure_agent"), patch.object(prod, "stop_api_jobs"), \
                patch.object(prod, "stop_process") as stop, patch.object(prod, "launch") as launch, \
                patch.object(prod, "wait_healthy"):
            prod.deploy(self.root, binary, config(), "nacos:8848", "Darwin")
            stop.assert_called_once_with(previous, "Darwin")
            self.assertTrue(all(call.args[0] == 30006 for call in listeners.call_args_list))
            self.assertEqual(launch.call_args.args[3]["FINANCE_SYS_ENV"], "PROD")
            self.assertEqual(launch.call_args.args[3]["NACOS_SERVER_ADDR"], "nacos:8848")
        state = (self.root / "tmp/production/active.json").read_text()
        self.assertEqual(json.loads(state)["pid"], 456)
        self.assertNotIn("private-value", state)

    def test_launchd_inspects_both_labels_before_stopping_any(self):
        own = self.root / "tmp/api_nacos"
        with patch.object(prod, "loaded_job_info", side_effect=[{"binary": own, "pid": 1}, {"binary": Path("/other/program"), "pid": 2}]), patch.object(prod, "run") as command:
            with self.assertRaises(prod.DeploymentError):
                prod.stop_api_jobs(self.root, "Darwin")
            command.assert_not_called()

    def test_legacy_launchd_test_process_is_never_stopped(self):
        same_binary = self.root / "tmp/api_nacos"
        job = {"binary": same_binary, "pid": 124}
        production = {"binary": str(same_binary), "pid": 123}
        with patch.object(prod, "loaded_job_info", side_effect=[None, job]), patch.object(prod, "run") as command:
            with self.assertRaisesRegex(prod.DeploymentError, "possible test service"):
                prod.stop_api_jobs(self.root, "Darwin", production)
            command.assert_not_called()

    def test_failed_new_api_restores_previous_binary_but_returns_failure(self):
        binary = self.root / "tmp/production/releases/new/finance-sys-api"
        binary.parent.mkdir(parents=True)
        binary.write_bytes(b"new")
        old_binary = self.root / "tmp/api_nacos"
        old_binary.write_bytes(b"old")
        previous = {"pid": 123, "binary": str(old_binary)}
        restored = {"pid": 456, "binary": str(old_binary)}
        with patch.object(prod, "listener", side_effect=[previous, None, restored]), \
                patch.object(prod, "ensure_agent"), patch.object(prod, "stop_api_jobs"), patch.object(prod, "stop_process"), \
                patch.object(prod, "launch") as launch, patch.object(prod, "stop_launched"), \
                patch.object(prod, "wait_healthy", side_effect=[prod.DeploymentError("new failed"), None]):
            with self.assertRaisesRegex(prod.DeploymentError, "Deployment failed"):
                prod.deploy(self.root, binary, config(), "nacos:8848", "Darwin")
            self.assertEqual(launch.call_args_list[1].args[0], [str(old_binary)])
        self.assertEqual(json.loads((self.root / "tmp/production/active.json").read_text())["pid"], 456)

    def test_plist_round_trip_handles_spaces_and_bootstrap_environment(self):
        spec = prod.make_plist(prod.API_LABEL, [self.root / "with space/api"], self.root,
                               {"FINANCE_SYS_ENV": "PROD", "NACOS_SERVER_ADDR": "nacos:8848"}, self.root / "logs")
        loaded = plistlib.loads(plistlib.dumps(spec))
        self.assertEqual(loaded["ProgramArguments"], [str(self.root / "with space/api")])
        self.assertEqual(loaded["EnvironmentVariables"]["FINANCE_SYS_ENV"], "PROD")
        self.assertEqual(loaded["KeepAlive"], {"SuccessfulExit": False})

    def test_launchd_plist_does_not_persist_inherited_secrets(self):
        environment = {"PATH": "/usr/bin", "NACOS_SERVER_ADDR": "nacos:8848", "FINANCE_SYS_ENV": "PROD",
                       "MY_PRIVATE_TOKEN": "secret-value", "DATABASE_PASSWORD": "db-secret"}
        with patch.object(Path, "home", return_value=self.root), patch.object(prod.os, "getuid", return_value=501, create=True), \
                patch.object(prod, "tool", side_effect=lambda name: name), patch.object(prod, "run"):
            prod.launch([self.root / "api"], prod.API_LABEL, self.root, environment, self.root / "logs", "Darwin")
        raw = (self.root / "Library/LaunchAgents" / (prod.API_LABEL + ".plist")).read_bytes()
        spec = plistlib.loads(raw)
        self.assertEqual(spec["EnvironmentVariables"]["FINANCE_SYS_ENV"], "PROD")
        self.assertNotIn(b"secret-value", raw)
        self.assertNotIn(b"db-secret", raw)

    def test_cleanup_will_not_stop_another_launchd_program(self):
        with patch.object(prod, "loaded_job", return_value=Path("/unrelated/app")), patch.object(prod, "run") as command:
            with self.assertRaises(prod.DeploymentError):
                prod.stop_launched(prod.API_LABEL, None, "Darwin", self.root / "api")
            command.assert_not_called()

    def test_concurrent_deployment_is_rejected(self):
        with prod.deployment_lock(self.root):
            with self.assertRaises(prod.DeploymentError):
                with prod.deployment_lock(self.root):
                    self.fail("second lock must fail")


if __name__ == "__main__":
    unittest.main()
