"""Runtime support for start_prod.py; not a second deployment entry point."""

from contextlib import contextmanager
import json
import csv
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import threading
import time


SEGMENT_BYTES = 100_000_000
HIGH_WATER_BYTES = 1_000_000_000
SEGMENT_NAME = re.compile(r"^[0-9]{20}\.log$")


@contextmanager
def file_lock(path, *, blocking=True):
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink():
        raise ValueError("Refusing a symlink lock file")
    with path.open("a+b") as handle:
        if os.name == "nt":
            import msvcrt
            if handle.tell() == 0:
                handle.write(b"\0")
                handle.flush()
            handle.seek(0)
            mode = msvcrt.LK_LOCK if blocking else msvcrt.LK_NBLCK
            msvcrt.locking(handle.fileno(), mode, 1)
            try:
                yield
            finally:
                handle.seek(0)
                msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
        else:
            import fcntl
            flags = fcntl.LOCK_EX | (0 if blocking else fcntl.LOCK_NB)
            fcntl.flock(handle.fileno(), flags)
            try:
                yield
            finally:
                fcntl.flock(handle.fileno(), fcntl.LOCK_UN)


class SegmentedLogs:
    """All services share one locked, size-bounded log series across releases."""

    def __init__(self, directory, segment_bytes=SEGMENT_BYTES, high_water_bytes=HIGH_WATER_BYTES):
        if segment_bytes <= 0 or high_water_bytes <= segment_bytes:
            raise ValueError("log high water must exceed the positive segment size")
        self.directory = Path(directory)
        self.segment_bytes = segment_bytes
        self.high_water_bytes = high_water_bytes
        self.directory.mkdir(parents=True, exist_ok=True)

    def _segments(self):
        paths = [p for p in self.directory.iterdir() if SEGMENT_NAME.fullmatch(p.name)]
        if any(p.is_symlink() or not p.is_file() for p in paths):
            raise ValueError("Refusing a non-regular log segment")
        return sorted(paths)

    def write(self, data):
        if not data:
            return
        with file_lock(self.directory / ".log.lock"):
            remaining = memoryview(data)
            while remaining:
                segments = self._segments()
                current = segments[-1] if segments else self.directory / ("%020d.log" % 1)
                size = current.stat().st_size if current.exists() else 0
                if size >= self.segment_bytes:
                    current = self.directory / ("%020d.log" % (int(current.stem) + 1))
                    size = 0
                amount = min(len(remaining), self.segment_bytes - size)
                with current.open("ab") as output:
                    output.write(remaining[:amount])
                remaining = remaining[amount:]
                segments = self._segments()
                if sum(p.stat().st_size for p in segments) >= self.high_water_bytes:
                    # No process holds a segment open outside the shared lock.
                    # At the threshold retain only the newest <=100 MB segment.
                    for old in segments[:-1]:
                        old.unlink()


def _alive(pid):
    if os.name == "nt":
        result = subprocess.run(["tasklist", "/FI", "PID eq %d" % pid, "/FO", "CSV", "/NH"],
                                capture_output=True, text=True, check=True)
        return any(len(row) > 1 and row[1] == str(pid) for row in csv.reader(result.stdout.splitlines()))
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False


def supervise(command, cwd, runtime_root, service, environment):
    """Own one child tree, clean only its workspace, and forward shutdown."""
    runtime_root = Path(runtime_root)
    logs = SegmentedLogs(runtime_root / "logs/runtime")
    state_path = runtime_root / (service + ".child.json")
    with file_lock(runtime_root / (service + ".runtime.lock"), blocking=False):
        if state_path.exists():
            previous = json.loads(state_path.read_text())
            if _alive(int(previous["pid"])):
                raise RuntimeError("Previous service child is still alive; refusing duplicate or workspace cleanup")
        workspace = runtime_root / "work" / service
        if workspace.is_symlink():
            raise RuntimeError("Service workspace must not be a symlink")
        if workspace.exists():
            shutil.rmtree(workspace)
        workspace.mkdir(parents=True)
        child_env = dict(environment)
        child_env.update(TMPDIR=str(workspace), TMP=str(workspace), TEMP=str(workspace), PYTHONUNBUFFERED="1")
        child = subprocess.Popen(
            [str(value) for value in command], cwd=str(cwd), env=child_env,
            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            start_new_session=os.name != "nt",
        )
        try:
            state_path.write_text(json.dumps({"pid": child.pid, "service": service}) + "\n")
        except OSError:
            if os.name == "nt":
                subprocess.run(["taskkill", "/PID", str(child.pid), "/T", "/F"], check=True)
            else:
                os.killpg(child.pid, signal.SIGKILL)
            child.wait(timeout=5)
            child.stdout.close()
            shutil.rmtree(workspace)
            raise
        errors = []
        stopping = threading.Event()

        def stop(signum=signal.SIGTERM, _frame=None):
            stopping.set()
            if child.poll() is None:
                if os.name == "nt":
                    subprocess.run(["taskkill", "/PID", str(child.pid), "/T", "/F"], check=True, stdout=subprocess.DEVNULL)
                else:
                    try:
                        os.killpg(child.pid, signum)
                    except ProcessLookupError:
                        pass  # The child exited between poll and the signal.

        def pump():
            try:
                while True:
                    data = child.stdout.read1(64 * 1024)
                    if not data:
                        break
                    logs.write(("[" + service + "] ").encode() + data)
            except (OSError, ValueError) as error:
                errors.append(error)

        handlers = {number: signal.signal(number, stop) for number in (signal.SIGTERM, signal.SIGINT)}
        reader = threading.Thread(target=pump, name="service-log-reader", daemon=True)
        reader.start()
        try:
            while child.poll() is None:
                if errors or stopping.is_set():
                    stop()
                    try:
                        child.wait(timeout=25)
                    except subprocess.TimeoutExpired:
                        stop(getattr(signal, "SIGKILL", signal.SIGTERM))
                        child.wait(timeout=5)
                    break
                time.sleep(0.1)
            if os.name != "nt":
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            reader.join(timeout=5)
            if reader.is_alive():
                raise RuntimeError("Service stdout remained open after child exit")
            if errors:
                raise RuntimeError("Service log capture failed") from errors[0]
            return 0 if stopping.is_set() else child.returncode
        finally:
            for number, handler in handlers.items():
                signal.signal(number, handler)
            if child.poll() is None:
                stop(getattr(signal, "SIGKILL", signal.SIGTERM))
                child.wait(timeout=5)
            child.stdout.close()
            if child.poll() is not None and not reader.is_alive():
                state_path.unlink()
                shutil.rmtree(workspace)


def report_runtime_failure(runtime_root, service, error):
    message = ("[supervisor:%s] %s: %s\n" % (service, type(error).__name__, error)).encode()
    SegmentedLogs(Path(runtime_root) / "logs/runtime").write(message)
