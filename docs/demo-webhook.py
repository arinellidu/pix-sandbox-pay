#!/usr/bin/env python3
"""A webhook receiver in the standard library, for the demo and for you.

    python3 docs/demo-webhook.py            # listens on 127.0.0.1:9099

Every callback is printed as one line of JSON. Set WEBHOOK_SECRET to the same
value the sandbox uses and each line also reports whether the signature
verified — which is the whole point of the header.
"""

import hashlib
import hmac
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

SECRET = os.environ.get("WEBHOOK_SECRET", "pix-sandbox").encode()


class Receiver(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("content-length", 0)))
        expected = hmac.new(SECRET, body, hashlib.sha256).hexdigest()
        signature = self.headers.get("X-Signature", "")

        self.send_response(200)
        self.end_headers()

        report = {
            "signature_ok": hmac.compare_digest(signature, expected),
            **json.loads(body or b"{}"),
        }
        print(json.dumps(report), flush=True)

    # The access log would drown the callbacks it is meant to show.
    def log_message(self, *args):
        pass


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 9099
    HTTPServer(("127.0.0.1", port), Receiver).serve_forever()
