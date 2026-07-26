from http.server import BaseHTTPRequestHandler
import time

import pytest

from app.http_client import HTTPTimeoutError, StdlibHTTPClient


class QuietHTTPHandler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        return


def test_stdlib_client_enforces_total_body_deadline(run_http_server):
    class SlowBodyHandler(QuietHTTPHandler):
        def do_POST(self):
            body = b"slow-response-body"
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            try:
                for byte in body:
                    self.wfile.write(bytes([byte]))
                    self.wfile.flush()
                    time.sleep(0.03)
            except (BrokenPipeError, ConnectionResetError):
                pass

    address = run_http_server(SlowBodyHandler)
    started = time.monotonic()
    with pytest.raises(HTTPTimeoutError):
        StdlibHTTPClient(timeout=0.12).post(f"http://{address}/slow", json={})
    assert time.monotonic() - started < 0.5
