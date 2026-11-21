import importlib.util
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import tempfile
import threading
import unittest
import urllib.error
import urllib.request

spec = importlib.util.spec_from_file_location("preview0938", Path(__file__).parents[1] / "scripts/preview_server_v0.9.3-8.py")
preview = importlib.util.module_from_spec(spec)
spec.loader.exec_module(preview)


class PreviewBoundaryTest(unittest.TestCase):
    def test_only_get_is_forwarded_and_state_is_read_only(self):
        calls = []

        class Upstream(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                calls.append(self.command)
                raw = json.dumps({"read_only": False, "config": {"enabled": True}}).encode()
                self.send_response(200)
                self.end_headers()
                self.wfile.write(raw)

        with tempfile.TemporaryDirectory() as directory:
            Path(directory, "index.html").write_text("<body>test</body>")
            upstream = ThreadingHTTPServer(("127.0.0.1", 0), Upstream)
            server = ThreadingHTTPServer(("127.0.0.1", 0), preview.handler(directory, "http://127.0.0.1:" + str(upstream.server_port)))
            for instance in [upstream, server]:
                threading.Thread(target=instance.serve_forever, daemon=True).start()
            base = "http://127.0.0.1:" + str(server.server_port)
            try:
                for method in ["POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE"]:
                    with self.assertRaises(urllib.error.HTTPError) as error:
                        urllib.request.urlopen(urllib.request.Request(base + "/api/config", data=b"{}", method=method))
                    self.assertEqual(error.exception.code, 409)
                self.assertEqual(calls, [])
                data = json.load(urllib.request.urlopen(base + "/api/state"))
                self.assertTrue(data["read_only"])
                self.assertEqual(data["config"], {"enabled": True})
                self.assertEqual(calls, ["GET"])
                self.assertIn(b"v0.9.3-8", urllib.request.urlopen(base + "/").read())
                with self.assertRaises(urllib.error.HTTPError) as error:
                    urllib.request.urlopen(base + "/%2e%2e/index.html")
                self.assertEqual(error.exception.code, 404)
            finally:
                server.shutdown(); server.server_close()
                upstream.shutdown(); upstream.server_close()


class PlannerBoundaryTest(unittest.TestCase):
    def test_only_firewall_plan_goes_to_isolated_planner(self):
        calls = []

        class Backend(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                calls.append(("GET", self.path))
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b'{"entries":[],"zone_policy":{"complete":true}}')

            def do_POST(self):
                calls.append(("POST", self.path))
                data = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                if data.get("operation") == "redirect":
                    self.send_response(302)
                    self.send_header("Location", "/api/network/apply")
                    self.end_headers()
                    return
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b'{"id":"preview","warnings":[],"preview":["example"]}')

        with tempfile.TemporaryDirectory() as directory:
            Path(directory, "index.html").write_text("<body>test</body>")
            planner = ThreadingHTTPServer(("127.0.0.1", 0), Backend)
            # Production is deliberately unreachable; firewall routes must never go there.
            server = ThreadingHTTPServer(("127.0.0.1", 0), preview.handler(directory, "http://127.0.0.1:1", "http://127.0.0.1:" + str(planner.server_port)))
            for instance in [planner, server]:
                threading.Thread(target=instance.serve_forever, daemon=True).start()
            base = "http://127.0.0.1:" + str(server.server_port)

            def post(path, data, extra=None):
                headers = {"Content-Type":"application/json"}
                headers.update(extra or {})
                return urllib.request.urlopen(urllib.request.Request(base + path, data=json.dumps(data).encode(), headers=headers))

            try:
                for path, data, code in [("/api/network/apply", {"kind":"firewall"}, 409), ("/api/network/plan?x=1", {"kind":"firewall"}, 409), ("/api/network/plan", {"kind":"routes"}, 409), ("/api/network/plan", [], 409)]:
                    with self.assertRaises(urllib.error.HTTPError) as error:
                        post(path, data)
                    self.assertEqual(error.exception.code, code)
                with self.assertRaises(urllib.error.HTTPError) as error:
                    post("/api/network/plan", {"kind":"firewall"}, {"Origin":"http://untrusted.example"})
                self.assertEqual(error.exception.code, 403)
                self.assertEqual(calls, [])
                data = json.load(post("/api/network/plan", {"kind":"firewall"}))
                self.assertTrue(data["preview_only"])
                self.assertEqual(calls, [("POST", "/api/network/plan")])
                data = json.load(urllib.request.urlopen(base + "/api/network/firewall?zone=public"))
                self.assertTrue(data["zone_policy"]["complete"])
                with self.assertRaises(urllib.error.HTTPError) as error:
                    post("/api/network/plan", {"kind":"firewall","operation":"redirect"})
                self.assertEqual(error.exception.code, 502)
                self.assertNotIn(("POST", "/api/network/apply"), calls)
                self.assertNotIn(("GET", "/api/network/apply"), calls)
            finally:
                server.shutdown(); server.server_close()
                planner.shutdown(); planner.server_close()


if __name__ == "__main__":
    unittest.main()
