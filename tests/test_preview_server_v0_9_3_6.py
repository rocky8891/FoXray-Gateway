import importlib.util
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import tempfile
import threading
import unittest
import urllib.error
import urllib.request

spec = importlib.util.spec_from_file_location("preview0936", Path(__file__).parents[1] / "scripts/preview_server_v0.9.3-6.py")
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
                self.assertIn(b"v0.9.3-6", urllib.request.urlopen(base + "/").read())
                with self.assertRaises(urllib.error.HTTPError) as error:
                    urllib.request.urlopen(base + "/%2e%2e/index.html")
                self.assertEqual(error.exception.code, 404)
            finally:
                server.shutdown(); server.server_close()
                upstream.shutdown(); upstream.server_close()


if __name__ == "__main__":
    unittest.main()
