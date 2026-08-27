"""
tests/unit/stub_gitea.py — a fake Gitea, just real enough to test policy-eval against.

The regression suite (tests/regression/) proves policy-eval against a REAL Gitea,
and that proof is the one that actually matters — nothing here replaces it.
What this adds is speed and zero privilege: no Docker, no containers, no
network beyond localhost, so this can safely run on every push to this
platform's own repo, on the ordinary agent, with no Docker-in-Docker
question to answer.

Deliberately NOT a mock of policy-eval's internals (no monkeypatching, no
modified import of verify-approvals.py). policy-eval runs as a real
subprocess, in its exact deployed form, and is pointed at this stub via
GITEA_URL like it would be pointed at a real Gitea -- the one thing under
test stays untouched by the act of testing it.

Serves canned JSON keyed by exact request path, on an ephemeral localhost
port. A path with no fixture returns 404, matching Gitea's own behavior
for a resource that doesn't exist (this is what lets branch_protections'
allow_404 path be tested honestly, by simply not providing a fixture for
it, rather than needing an explicit "not found" sentinel).
"""
import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer


class _Handler(BaseHTTPRequestHandler):
    fixtures = {}

    def do_GET(self):
        if self.path in self.fixtures:
            status, body = self.fixtures[self.path]
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(body).encode())
        else:
            self.send_response(404)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"message": "not found"}).encode())

    def log_message(self, *args):
        pass  # keep test output readable -- fixture mismatches show up as assertion failures, not noise


class StubGitea:
    """Usage:
        stub = StubGitea({"/api/v1/repos/o/r/pulls/1": (200, {...}), ...})
        stub.start()
        ... GITEA_URL = stub.url ...
        stub.stop()
    """

    def __init__(self, fixtures):
        handler = type("Handler", (_Handler,), {"fixtures": fixtures})
        self._server = HTTPServer(("127.0.0.1", 0), handler)
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)

    @property
    def url(self):
        return f"http://127.0.0.1:{self._server.server_port}"

    def start(self):
        self._thread.start()

    def stop(self):
        self._server.shutdown()
        self._server.server_close()
