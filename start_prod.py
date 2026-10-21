#!/usr/bin/env python3
"""Build and deploy production on macOS/Windows; --build-only never starts services.

Only the Nacos address is bootstrap configuration. Credentials stay in memory and
are never written to release manifests, launchd plists, or command arguments.
"""

import argparse
import base64
from contextlib import contextmanager
from datetime import datetime, timezone
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
from urllib.parse import urlencode, urlsplit
from urllib.request import ProxyHandler, Request, build_opener


ROOT = Path(__file__).resolve().parent
API_LABEL = "cn.financesys.api.production"
AGENT_LABEL = "cn.financesys.agent.production"
LEGACY_API_LABEL = "com.loliwolf.finance-sys.api"
SIGNING_IDENTIFIER = API_LABEL
LSREGISTER = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
HTTP = build_opener(ProxyHandler({}))  # Direct LAN requests, independent of proxy env.


class DeploymentError(RuntimeError):
    pass


def info(message):
    print("[INFO] " + message, flush=True)


def run(argv, *, cwd=ROOT, env=None, capture=False, check=True, timeout=None):
    argv = [str(value) for value in argv]
    if os.name == "nt" and argv[0].lower().endswith((".cmd", ".bat")):
        argv = ["cmd.exe", "/d", "/s", "/c", subprocess.list2cmdline(argv)]
    result = subprocess.run(
        argv, cwd=str(cwd), env=env, check=False, timeout=timeout,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.STDOUT if capture else None,
        text=True, encoding="utf-8", errors="replace",
    )
    if check and result.returncode:
        detail = (result.stdout or "").strip() if capture else "see output above"
        raise DeploymentError("{} failed ({}): {}".format(Path(argv[0]).name, result.returncode, detail))
    return result


def tool(name):
    path = shutil.which(name)
    if not path:
        raise DeploymentError("Required command not found: " + name)
    return path


def powershell(script):
    script = "$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false); " + script
    encoded = base64.b64encode(script.encode("utf-16le")).decode("ascii")
    return run([tool("powershell.exe"), "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded], capture=True).stdout


def ps_quote(value):
    return "'" + str(value).replace("'", "''") + "'"


def nacos_address(root, explicit_file=None, environ=None):
    environ = os.environ if environ is None else environ
    if not explicit_file and environ.get("NACOS_SERVER_ADDR", "").strip():
        return environ["NACOS_SERVER_ADDR"].strip()
    if explicit_file:
        path = Path(explicit_file)
        if not path.is_absolute():
            path = root / path
    else:
        path = root / "bootstrap_go122.env"
        if not path.is_file():
            path = root / "bootstrap_go122.env.example"
    address = None
    for raw in path.read_text(encoding="utf-8-sig").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        key, separator, value = line.partition("=")
        if not separator or key.strip() != "NACOS_SERVER_ADDR":
            raise DeploymentError("Bootstrap file may contain only NACOS_SERVER_ADDR: " + str(path))
        if address is not None:
            raise DeploymentError("Duplicate NACOS_SERVER_ADDR in " + str(path))
        address = value.strip().strip("\"'").strip()
    if not address:
        raise DeploymentError("NACOS_SERVER_ADDR is empty in " + str(path))
    return address


def read_json_url(url, timeout=10):
    with HTTP.open(Request(url, headers={"Accept": "application/json"}), timeout=timeout) as response:
        return json.loads(response.read())


def check_port(value, name):
    if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= 65535:
        raise DeploymentError(name + " must be an integer in 1..65535")
    return value


def validate_config(cfg):
    if not isinstance(cfg, dict):
        raise DeploymentError("Nacos configuration must be a JSON object")
    http = cfg.get("service", {}).get("http", {})
    port = check_port(http.get("port"), "service.http.port")
    test_port = check_port(http.get("port_test"), "service.http.port_test")
    if port == test_port:
        raise DeploymentError("Production and test ports must be different")
    # Fail closed before any process is stopped, without ever logging the DSN.
    database = cfg.get("database", {})
    match = re.search(r"@tcp\([^)]+\)/([^?]+)(?:\?|$)", str(database.get("dsn") or ""))
    if database.get("driver") != "mysql" or not match or match.group(1) != "expert_trade":
        raise DeploymentError("Nacos production database must be mysql / expert_trade")
    endpoint = urlsplit(cfg.get("agent", {}).get("endpoint", ""))
    if endpoint.scheme not in ("http", "https") or not endpoint.hostname or endpoint.username or endpoint.password:
        raise DeploymentError("agent.endpoint must be an HTTP(S) URL without credentials")
    check_port(endpoint.port or (443 if endpoint.scheme == "https" else 80), "agent.endpoint port")
    return cfg


def deployment_targets(cfg):
    http = cfg["service"]["http"]
    return (http.get("host"), http["port"], http["port_test"], cfg["agent"]["endpoint"])


def load_nacos(address):
    query = urlencode({"dataId": "expert_trade", "group": "DEFAULT_GROUP", "tenant": "public"})
    errors = []
    for server in address.split(","):
        server = server.strip()
        parsed = urlsplit("http://" + server)
        if not parsed.hostname or parsed.username or parsed.password or parsed.path or not parsed.port:
            raise DeploymentError("NACOS_SERVER_ADDR must contain host:port values")
        try:
            cfg = read_json_url("http://" + server + "/nacos/v1/cs/configs?" + query)
            validate_config(cfg)
            grpc_port = check_port(parsed.port + 1000, "Nacos gRPC port")
            with socket.create_connection((parsed.hostname, grpc_port), timeout=3):
                pass
            info("Nacos HTTP and SDK gRPC reachable; config version {}".format(cfg.get("meta", {}).get("config_version")))
            return cfg
        except DeploymentError:
            raise
        except Exception as exc:
            # Do not echo a remote response that could contain configuration secrets.
            errors.append("{} ({})".format(server, type(exc).__name__))
    raise DeploymentError("Cannot read production config / reach Nacos SDK: " + ", ".join(errors))


def clean_environment(environ=None, production=False, address=None):
    result = dict(os.environ if environ is None else environ)
    for key in list(result):
        if key.startswith(("FINANCE_SYS_", "NACOS_", "VITE_")) or key in ("GOOS", "GOARCH", "APP_PORT", "APP_BASE_URL"):
            result.pop(key)
    result.update(FINANCE_SYS_ENV="PROD" if production else "TEST", GOTOOLCHAIN="local", GOWORK="off", GOFLAGS="", OPEN_UPLOAD_PAGE="0")
    if address:
        result["NACOS_SERVER_ADDR"] = address
    return result


@contextmanager
def deployment_lock(directory):
    directory.mkdir(parents=True, exist_ok=True)
    with (directory / "deploy.lock").open("a+") as handle:
        if os.name == "nt":
            import msvcrt
            handle.seek(0, 2)
            if handle.tell() == 0:
                handle.write("0")
                handle.flush()
            handle.seek(0)
            try:
                msvcrt.locking(handle.fileno(), msvcrt.LK_NBLCK, 1)
            except OSError as exc:
                raise DeploymentError("Another production build/deployment is running") from exc
            try:
                yield
            finally:
                handle.seek(0)
                msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
        else:
            import fcntl
            try:
                fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            except OSError as exc:
                raise DeploymentError("Another production build/deployment is running") from exc
            try:
                yield
            finally:
                fcntl.flock(handle.fileno(), fcntl.LOCK_UN)


def write_json(path, value):
    fd, temporary = tempfile.mkstemp(prefix=path.name + ".", dir=str(path.parent))
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        json.dump(value, handle, ensure_ascii=False, indent=2)
        handle.write("\n")
    os.replace(temporary, path)


def sign_macos(binary):
    target = macos_bundle(binary) or binary
    if macos_bundle(binary) and macos_entry(binary) != binary:
        run([tool("codesign"), "--force", "--sign", "-", "--identifier", SIGNING_IDENTIFIER + ".api", "--timestamp=none", binary])
    run([tool("codesign"), "--force", "--sign", "-", "--identifier", SIGNING_IDENTIFIER, "--timestamp=none", target])
    run([tool("codesign"), "--verify", "--strict", "--verbose=2", target])
    identity = run([tool("codesign"), "-dv", "--verbose=4", target], capture=True).stdout
    if "Identifier=" + SIGNING_IDENTIFIER + "\n" not in identity or "Signature=adhoc" not in identity:
        raise DeploymentError("macOS ad-hoc signing identity verification failed")
    commands = run([tool("otool"), "-l", binary], capture=True).stdout
    uuids = re.findall(r"cmd LC_UUID\s+cmdsize \d+\s+uuid ([0-9A-Fa-f-]{36})", commands)
    if len(uuids) != 1 or uuids[0].replace("-", "") == "0" * 32:
        raise DeploymentError("Binary must contain exactly one nonzero Mach-O LC_UUID")
    info("macOS signature verified: {} / UUID {}".format(SIGNING_IDENTIFIER, uuids[0]))
    return uuids[0]


def macos_bundle(binary):
    binary = Path(binary)
    if binary.parent.name == "MacOS" and binary.parent.parent.name == "Contents" and binary.parents[2].suffix == ".app":
        return binary.parents[2]
    return None


def release_directory(binary):
    bundle = macos_bundle(binary)
    return bundle.parent if bundle else binary.parent


def macos_entry(binary):
    launcher = Path(binary).parent / "FinanceSysLauncher"
    return launcher if macos_bundle(binary) and launcher.is_file() else Path(binary)


def prepare_macos_bundle(release):
    bundle = release / "FinanceSys.app"
    executable_dir = bundle / "Contents/MacOS"
    executable_dir.mkdir(parents=True)
    spec = {"CFBundleIdentifier": SIGNING_IDENTIFIER, "CFBundleExecutable": "FinanceSysLauncher",
            "CFBundleName": "FinanceSys", "CFBundleDisplayName": "FinanceSys 生产服务", "CFBundlePackageType": "APPL",
            "CFBundleVersion": "1", "LSUIElement": True,
            "NSLocalNetworkUsageDescription": "FinanceSys 需要连接局域网内的 Nacos 配置中心和 MySQL 数据库，为研迹提供文档报告与追踪服务。"}
    with (bundle / "Contents/Info.plist").open("wb") as handle:
        plistlib.dump(spec, handle)
    return executable_dir / "finance-sys-api"


def build_release(root, system):
    npm, go = tool("npm.cmd" if system == "Windows" else "npm"), tool("go")
    if system == "Darwin":
        tool("codesign")
        tool("otool")
        tool("xcrun")
        help_text = run([go, "tool", "link", "-h"], capture=True, check=False).stdout
        if "Mach-O UUID" not in help_text or "gobuildid" not in help_text:
            raise DeploymentError("The installed Go linker cannot generate Mach-O UUID with -B gobuildid")
    environment = clean_environment()
    info("Run deployment regression tests and frontend/Go gates (TEST environment)")
    run([sys.executable, "-B", "-m", "unittest", "discover", "-s", "scripts/tests", "-p", "test_start_prod.py"], cwd=root, env=environment)
    run([npm, "ci", "--no-audit", "--no-fund"], cwd=root / "frontend", env=environment)
    run([npm, "run", "typecheck"], cwd=root / "frontend", env=environment)
    run([npm, "test"], cwd=root / "frontend", env=environment)
    run([npm, "run", "build"], cwd=root / "frontend", env=environment)
    run([go, "test", "./..."], cwd=root, env=environment)
    run([go, "build", "./..."], cwd=root, env=environment)

    releases = root / "tmp/production/releases"
    releases.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ-")
    release = Path(tempfile.mkdtemp(prefix=stamp, dir=str(releases)))
    binary = prepare_macos_bundle(release) if system == "Darwin" else release / "finance-sys-api.exe"
    command = [go, "build"]
    if system == "Darwin":
        command.append("-ldflags=-B=gobuildid")
    run(command + ["-o", binary, "./cmd/api"], cwd=root, env=environment)
    if system == "Darwin":
        run([tool("xcrun"), "clang", "-fobjc-arc", "-Wall", "-Wextra", "-Werror",
             "-framework", "Foundation", "-framework", "AppKit", "-framework", "Network",
             root / "scripts/macos/api_launcher.m", "-o", binary.parent / "FinanceSysLauncher"], cwd=root, env=environment)
    uuid = sign_macos(binary) if system == "Darwin" else None
    digest = hashlib.sha256()
    with binary.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    manifest = {"binary": str(binary), "platform": system, "sha256": digest.hexdigest(), "mach_o_uuid": uuid,
                "signing_identifier": SIGNING_IDENTIFIER if system == "Darwin" else None,
                "signature": "adhoc" if system == "Darwin" else "none", "built_at": datetime.now(timezone.utc).isoformat()}
    write_json(release / "manifest.json", manifest)
    info("Production artifact ready: " + str(binary))
    return binary


def healthy(url):
    try:
        return read_json_url(url, timeout=2).get("status") == "ok"
    except Exception:
        return False


def wait_healthy(url, timeout=60):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if healthy(url):
            return
        time.sleep(0.5)
    raise DeploymentError("Health check failed: " + url + "; inspect production logs (including macOS Local Network permission)")


def api_url(cfg):
    http = cfg["service"]["http"]
    host = http.get("host") or "127.0.0.1"
    if host in ("0.0.0.0", "::"):
        host = "127.0.0.1"
    if ":" in host:
        host = "[" + host + "]"
    return "http://{}:{}/healthz".format(host, http["port"])


def process_path(pid, system):
    if system == "Windows":
        raw = powershell("$p=Get-CimInstance Win32_Process -Filter \"ProcessId={}\"; if ($p) {{ $p.ExecutablePath }}".format(int(pid)))
    else:
        raw = run([tool("ps"), "-p", str(pid), "-o", "comm="], capture=True, check=False).stdout
    value = raw.strip()
    return Path(value).resolve() if value else None


def managed_binary(path, root):
    if not path:
        return False
    path = Path(path).resolve()
    if path in ((root / "tmp/api_nacos").resolve(), (root / "tmp/api_nacos.exe").resolve()):
        return True  # One-time migration; only the actual production port is considered.
    releases = (root / "tmp/production/releases").resolve()
    release = release_directory(path)
    return release.parent == releases and path.name in ("finance-sys-api", "finance-sys-api.exe", "FinanceSysLauncher") and (not macos_bundle(path) or macos_bundle(path).name == "FinanceSys.app")


def listener(port, root, system):
    if system == "Windows":
        raw = powershell("@(Get-NetTCPConnection -State Listen -LocalPort {} -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess -Unique) | ConvertTo-Json -Compress".format(int(port)))
        ids = json.loads(raw) if raw.strip() else []
        if isinstance(ids, int):
            ids = [ids]
    else:
        result = run([tool("lsof"), "-nP", "-t", "-iTCP:" + str(port), "-sTCP:LISTEN"], capture=True, check=False)
        if result.returncode not in (0, 1):
            raise DeploymentError("Could not inspect production port")
        ids = [int(value) for value in result.stdout.split()]
    ids = sorted(set(ids))
    if len(ids) > 1:
        raise DeploymentError("Multiple processes own the production port; refusing to stop any")
    if not ids:
        return None
    path = process_path(ids[0], system)
    if not managed_binary(path, root):
        raise DeploymentError("Production port is occupied by an unmanaged process (PID {})".format(ids[0]))
    return {"pid": ids[0], "binary": str(path)}


def same_process(process, system):
    return process_path(process["pid"], system) == Path(process["binary"]).resolve()


def stop_process(process, system, timeout=25):
    # Recheck the executable to protect against PID reuse between inspection and stop.
    if not same_process(process, system):
        return
    if system == "Windows":
        run([tool("taskkill"), "/PID", str(process["pid"]), "/T", "/F"])
    else:
        os.kill(process["pid"], signal.SIGTERM)
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if not same_process(process, system):
            return
        time.sleep(0.2)
    raise DeploymentError("Managed process did not stop; refusing to start a duplicate")


def launchd_target(label):
    return "gui/{}/{}".format(os.getuid(), label)


def loaded_job_info(label):
    result = run([tool("launchctl"), "print", launchd_target(label)], capture=True, check=False)
    if result.returncode:
        return None
    match = re.search(r"^\s*program = (.+)$", result.stdout, re.MULTILINE)
    if not match:
        raise DeploymentError("Cannot verify program of launchd job " + label)
    pid = re.search(r"^\s*pid = (\d+)$", result.stdout, re.MULTILINE)
    return {"binary": Path(match.group(1).strip()).resolve(), "pid": int(pid.group(1)) if pid else None}


def loaded_job(label):
    job = loaded_job_info(label)
    return job["binary"] if job else None


def stop_api_jobs(root, system, production_process=None):
    if system != "Darwin":
        return
    # Inspect all targets before stopping the first one.
    jobs = [(label, loaded_job_info(label)) for label in (API_LABEL, LEGACY_API_LABEL)]
    for label, job in jobs:
        if job and not managed_binary(job["binary"], root):
            raise DeploymentError("Unmanaged launchd label; refusing to replace " + label)
        if label == LEGACY_API_LABEL and job and (not production_process or job["pid"] != production_process["pid"]):
            raise DeploymentError("Legacy launchd job does not own the production listener; refusing to stop a possible test service")
    for label, job in jobs:
        if job:
            run([tool("launchctl"), "bootout", launchd_target(label)])


def make_plist(label, command, cwd, environment, logs):
    spec = {"Label": label, "ProgramArguments": [str(item) for item in command], "WorkingDirectory": str(cwd),
            "EnvironmentVariables": environment, "RunAtLoad": True,
            "KeepAlive": {"SuccessfulExit": False}, "ThrottleInterval": 10,
            "StandardOutPath": str(logs / (label + ".stdout.log")),
            "StandardErrorPath": str(logs / (label + ".stderr.log"))}
    if label == API_LABEL and macos_bundle(command[0]):
        spec["AssociatedBundleIdentifiers"] = [SIGNING_IDENTIFIER]
    return spec


def launch(command, label, cwd, environment, logs, system):
    logs.mkdir(parents=True, exist_ok=True)
    if system == "Darwin":
        bundle = macos_bundle(command[0])
        if bundle:
            run([LSREGISTER, "-f", bundle])
            command = [macos_entry(command[0])] + list(command[1:])
        directory = Path.home() / "Library/LaunchAgents"
        directory.mkdir(parents=True, exist_ok=True)
        target = directory / (label + ".plist")
        # Full inherited env may contain secrets; only explicit service bootstrap vars.
        keys = ("PATH", "NACOS_SERVER_ADDR", "FINANCE_SYS_ENV", "GOTOOLCHAIN", "OPEN_UPLOAD_PAGE", "PYTHONPATH")
        service_env = {key: environment[key] for key in keys if key in environment}
        spec = make_plist(label, command, cwd, service_env, logs)
        with target.open("wb") as handle:
            plistlib.dump(spec, handle)
        run([tool("plutil"), "-lint", target])
        run([tool("launchctl"), "bootstrap", "gui/" + str(os.getuid()), target])
        return None
    with (logs / (label + ".stdout.log")).open("ab") as stdout, (logs / (label + ".stderr.log")).open("ab") as stderr:
        child = subprocess.Popen(
            [str(value) for value in command], cwd=str(cwd), env=environment,
            stdin=subprocess.DEVNULL, stdout=stdout, stderr=stderr,
            creationflags=subprocess.DETACHED_PROCESS | subprocess.CREATE_NEW_PROCESS_GROUP,
        )
    return {"pid": child.pid, "binary": str(Path(command[0]).resolve())}


def stop_launched(label, process, system, expected_binary):
    if system == "Darwin":
        path = loaded_job(label)
        if path == macos_entry(expected_binary).resolve():
            run([tool("launchctl"), "bootout", launchd_target(label)])
        elif path:
            raise DeploymentError("Launchd job identity changed; refusing to stop " + label)
    elif process:
        stop_process(process, system)


def stop_verified_agent(cfg, root, system):
    """Explicit operator opt-in only; never stop a merely similarly named Python."""
    endpoint = urlsplit(cfg["agent"]["endpoint"])
    try:
        local = endpoint.hostname == "localhost" or ipaddress.ip_address(endpoint.hostname).is_loopback
    except ValueError:
        local = False
    if not local:
        raise DeploymentError("Explicit Agent restart supports only this host's loopback Agent")
    if system != "Darwin":
        raise DeploymentError("Explicit Agent restart requires verified launchd ownership on this host")
    port = endpoint.port or 80
    result = run([tool("lsof"), "-nP", "-t", "-iTCP:" + str(port), "-sTCP:LISTEN"], capture=True, check=False)
    pids = sorted(set(int(value) for value in result.stdout.split()))
    if len(pids) != 1:
        raise DeploymentError("Cannot uniquely verify the analysis Agent listener")
    expected = str(root / "agent/.venv/bin/python")
    if not Path(expected).is_file():
        raise DeploymentError("Analysis Agent virtualenv is missing")
    # macOS framework Python re-execs into Python.app, so compare with a child
    # of the exact venv interpreter rather than guessing the framework path.
    probe = run([expected, "-I", "-c", "import os,subprocess; print(subprocess.check_output(['/bin/ps','-p',str(os.getpid()),'-o','comm='],text=True).strip())"], capture=True)
    actual_expected = Path(probe.stdout.strip()).resolve()
    if process_path(pids[0], system) != actual_expected:
        raise DeploymentError("Agent listener executable does not match the project interpreter")
    verified = []
    for label in (AGENT_LABEL, "com.loliwolf.finance-sys.agent"):
        raw = run([tool("launchctl"), "print", launchd_target(label)], capture=True, check=False)
        if raw.returncode:
            continue
        pid = re.search(r"^\s*pid = (\d+)$", raw.stdout, re.MULTILINE)
        args = re.search(r"arguments = \{\n(.*?)\n\s*\}", raw.stdout, re.DOTALL)
        argv = [line.strip() for line in args.group(1).splitlines()] if args else []
        if not pid or int(pid.group(1)) != pids[0] or len(argv) < 3 or argv[-3:] != [expected, "-m", "app.runner"]:
            raise DeploymentError("Unverified Agent job; refusing to restart " + label)
        if len(argv) > 3:
            allowed = ("NACOS_SERVER_ADDR=", "FINANCE_SYS_ENV=", "PYTHONPATH=", "PATH=")
            if argv[0] != "/usr/bin/env" or any(not value.startswith(allowed) for value in argv[1:-3]):
                raise DeploymentError("Unrecognized Agent launcher arguments")
            supplied = dict(value.split("=", 1) for value in argv[1:-3])
            if supplied.get("PYTHONPATH") != str(root / "agent") or supplied.get("FINANCE_SYS_ENV") != "PROD":
                raise DeploymentError("Agent launcher is not the production project module")
        elif (str(root / "agent") not in raw.stdout or not re.search(r"FINANCE_SYS_ENV\s*(?:=>|=)\s*PROD", raw.stdout)):
            raise DeploymentError("Cannot verify Agent module directory and production environment")
        verified.append((label, raw.stdout, argv))
    if len(verified) != 1:
        raise DeploymentError("Agent listener does not have exactly one verified launchd owner")
    label, before, before_argv = verified[0]
    latest = run([tool("launchctl"), "print", launchd_target(label)], capture=True, check=False)
    before_pid = re.search(r"^\s*pid = (\d+)$", before, re.MULTILINE).group(1)
    latest_args = re.search(r"arguments = \{\n(.*?)\n\s*\}", latest.stdout, re.DOTALL)
    latest_argv = [line.strip() for line in latest_args.group(1).splitlines()] if latest_args else []
    if latest.returncode or not re.search(r"^\s*pid = " + before_pid + r"$", latest.stdout, re.MULTILINE) or latest_argv != before_argv or process_path(pids[0], system) != actual_expected:
        raise DeploymentError("Agent identity changed before restart")
    info("Explicitly upgrade verified analysis Agent PID " + before_pid)
    run([tool("launchctl"), "bootout", launchd_target(label)])
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        current = run([tool("lsof"), "-nP", "-t", "-iTCP:" + str(port), "-sTCP:LISTEN"], capture=True, check=False)
        if not current.stdout.strip():
            return
        if current.stdout.strip() != before_pid:
            raise DeploymentError("Agent port was taken by another process during restart")
        time.sleep(0.25)
    raise DeploymentError("Verified Agent listener did not stop")


def ensure_agent(cfg, root, environment, logs, system, restart=False):
    endpoint = urlsplit(cfg["agent"]["endpoint"])
    url = "{}://{}/healthz".format(endpoint.scheme, endpoint.netloc)
    if restart:
        stop_verified_agent(cfg, root, system)
    elif healthy(url):
        info("Reuse healthy analysis Agent: " + url)
        return
    try:
        local = endpoint.hostname == "localhost" or ipaddress.ip_address(endpoint.hostname).is_loopback
    except ValueError:
        local = False
    if not local:
        raise DeploymentError("Remote/non-loopback Agent is unavailable; start it on its own host: " + url)
    port = endpoint.port or (443 if endpoint.scheme == "https" else 80)
    try:
        with socket.create_connection((endpoint.hostname, port), timeout=2):
            raise DeploymentError("Agent port is occupied but unhealthy; refusing to kill/restart an unknown service")
    except OSError:
        pass
    python = root / "agent/.venv" / ("Scripts/python.exe" if system == "Windows" else "bin/python")
    if not python.is_file():
        raise DeploymentError("Analysis Agent virtualenv missing: " + str(python))
    if system == "Darwin" and any(loaded_job(label) for label in (AGENT_LABEL, "com.loliwolf.finance-sys.agent")):
        raise DeploymentError("Analysis Agent launchd job already exists but is unhealthy; inspect its logs first")
    child_env = dict(environment, PYTHONPATH=str(root / "agent"))
    process = launch([python, "-m", "app.runner"], AGENT_LABEL, root / "agent", child_env, logs, system)
    try:
        wait_healthy(url)
    except Exception:
        stop_launched(AGENT_LABEL, process, system, python)
        raise
    info("Analysis Agent is healthy: " + url)


def deploy(root, binary, cfg, address, system, restart_agent=False):
    environment = clean_environment(production=True, address=address)
    port = cfg["service"]["http"]["port"]
    previous = listener(port, root, system)
    logs = root / "tmp/production/logs" / release_directory(binary).name
    if restart_agent:
        ensure_agent(cfg, root, environment, logs, system, restart=True)
    else:
        ensure_agent(cfg, root, environment, logs, system)
    stop_api_jobs(root, system, previous)
    if previous:
        info("Stop verified production API PID {} (test port is untouched)".format(previous["pid"]))
        stop_process(previous, system)
    if listener(port, root, system):
        raise DeploymentError("Production port did not become free")
    process = None
    try:
        process = launch([binary], API_LABEL, root, environment, logs, system)
        wait_healthy(api_url(cfg))
        current = listener(port, root, system)
        if not current or Path(current["binary"]) != binary.resolve():
            raise DeploymentError("Health response did not come from the new production executable")
        write_json(root / "tmp/production/active.json", dict(current, port=port, logs=str(logs)))
    except Exception as original:
        stop_launched(API_LABEL, process, system, binary)
        if previous and Path(previous["binary"]).is_file():
            info("New API failed; attempting rollback to the previous executable")
            restored = None
            try:
                restored = launch([previous["binary"]], API_LABEL, root, environment, logs, system)
                wait_healthy(api_url(cfg))
                current = listener(port, root, system)
                if not current or Path(current["binary"]) != Path(previous["binary"]).resolve():
                    raise DeploymentError("Rollback health response came from a different executable")
                write_json(root / "tmp/production/active.json", dict(current, port=port, logs=str(logs)))
                info("Previous API restored; no database migration or configuration rollback was performed")
            except Exception as rollback_error:
                stop_launched(API_LABEL, restored, system, previous["binary"])
                raise DeploymentError("Deployment and rollback failed; logs: {} ({})".format(logs, rollback_error)) from original
        raise DeploymentError("Deployment failed; logs: {} ({})".format(logs, original)) from original
    info("Production ready: {} ; logs: {}".format(api_url(cfg), logs))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--build-only", action="store_true", help="test/build/sign only; no Nacos request, process start/stop, or launchd changes")
    mode.add_argument("--status", action="store_true", help="read Nacos and show production API/Agent health without changing anything")
    parser.add_argument("--env-file", help="bootstrap file containing only NACOS_SERVER_ADDR; overrides the address environment variable")
    parser.add_argument("--restart-agent", action="store_true", help="explicitly upgrade the verified local analysis Agent after build/sign gates; default still reuses a healthy Agent")
    args = parser.parse_args(argv)
    if args.restart_agent and (args.build_only or args.status):
        parser.error("--restart-agent cannot be combined with --build-only or --status")
    system = platform.system()
    if system not in ("Darwin", "Windows"):
        raise DeploymentError("This production launcher supports macOS and Windows")
    if args.status:
        cfg = load_nacos(nacos_address(ROOT, args.env_file))
        current = listener(cfg["service"]["http"]["port"], ROOT, system)
        endpoint = urlsplit(cfg["agent"]["endpoint"])
        print(json.dumps({"environment": "PROD", "api": current, "api_healthy": healthy(api_url(cfg)),
                          "agent_healthy": healthy("{}://{}/healthz".format(endpoint.scheme, endpoint.netloc))}, indent=2))
        return
    with deployment_lock(ROOT / "tmp/production"):
        address = None
        if not args.build_only:
            address = nacos_address(ROOT, args.env_file)
            cfg = load_nacos(address)
            targets_before_build = deployment_targets(cfg)
            listener(cfg["service"]["http"]["port"], ROOT, system)
            if system == "Darwin":
                for command in ("launchctl", "plutil", "lsof", "ps"):
                    tool(command)
                run([tool("launchctl"), "print", "gui/" + str(os.getuid())], capture=True)
        binary = build_release(ROOT, system)
        if args.build_only:
            info("Build-only complete. No service was started/stopped; no Nacos or database config was read/written.")
            return
        # Read again after a potentially long build. Never stop using stale port config.
        cfg = load_nacos(address)
        if deployment_targets(cfg) != targets_before_build:
            raise DeploymentError("Production/Agent address or ports changed during the build; no service was stopped. Run again after confirming Nacos.")
        deploy(ROOT, binary, cfg, address, system, restart_agent=args.restart_agent)


if __name__ == "__main__":
    try:
        main()
    except (DeploymentError, OSError, ValueError, subprocess.SubprocessError) as error:
        print("[ERROR] " + str(error), file=sys.stderr)
        sys.exit(1)
