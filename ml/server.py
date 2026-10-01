import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class Handler(BaseHTTPRequestHandler):
    def send_json(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            return self.send_json(200, {"status": "ok", "service": "ml-stub"})
        self.send_json(404, {"error": "not_found"})

    def do_POST(self):
        if self.path != "/predict":
            return self.send_json(404, {"error": "not_found"})
        length = int(self.headers.get("Content-Length", "0"))
        if length <= 0 or length > 1000000:
            return self.send_json(400, {"error": "invalid_request"})
        payload = json.loads(self.rfile.read(length))
        return self.send_json(200, {
            "status": "stub_only",
            "service": "ml",
            "received": payload,
            "note": "Integration placeholder; add the ML team's implementation."
        })

    def log_message(self, fmt, *args):
        print("ml-stub:", fmt % args)

ThreadingHTTPServer(("0.0.0.0", 8002), Handler).serve_forever()
