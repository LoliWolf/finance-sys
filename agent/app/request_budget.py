"""Cooperative deadline/cancellation shared by graph nodes and HTTP transports."""
from contextvars import ContextVar
from contextlib import contextmanager
import socket
import threading
import time


class RequestCancelled(RuntimeError):
    pass


class RequestBudget:
    def __init__(self, seconds):
        self.deadline = time.monotonic() + max(0, seconds)
        self.cancelled = threading.Event()
        self._lock = threading.Lock()
        self._closers = set()

    def remaining(self):
        if self.cancelled.is_set():
            raise RequestCancelled("request cancelled by caller")
        value = self.deadline - time.monotonic()
        if value <= 0:
            raise RequestCancelled("shared request deadline exceeded")
        return value

    def cancel(self):
        self.cancelled.set()
        with self._lock:
            closers = list(self._closers)
        for close in closers:
            close()

    @contextmanager
    def watch_response(self, response):
        # Shutting down the socket wakes a thread blocked in an SSE read.
        raw = getattr(getattr(response, "fp", None), "raw", None)
        sock = getattr(raw, "_sock", None)
        def close():
            if sock is not None:
                try:
                    sock.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass
        with self._lock:
            self._closers.add(close)
        try:
            self.remaining()
            yield
            self.remaining()
        finally:
            with self._lock:
                self._closers.discard(close)


current_budget = ContextVar("finance_request_budget", default=None)


def remaining_timeout(seconds):
    budget = current_budget.get()
    return min(seconds, budget.remaining()) if budget else seconds
