"""FoXray UI preview v0.9.3-10. See preview_server_v0.9.3-10_README.md."""
import argparse
import ipaddress
import json
import mimetypes
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import urllib.error
import urllib.parse
import urllib.request

VERSION = "0.9.3-10"


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def handler(web, upstream, planner=None):
    web = Path(web).resolve()
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    class Preview(BaseHTTPRequestHandler):
        def log_message(self, format, *args):
            pass  # Do not persist API queries or connection metadata.

        def reply(self, status, raw, content_type="application/json; charset=utf-8"):
            self.send_response(status)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(raw)))
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Content-Type-Options", "nosniff")
            self.send_header("X-Frame-Options", "SAMEORIGIN")
            self.send_header("Referrer-Policy", "same-origin")
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(raw)

        def denied(self):
            self.reply(409, json.dumps({"error": "只读预览，不执行服务端变更"}, ensure_ascii=False).encode())

        def private_client(self):
            address = ipaddress.ip_address(self.client_address[0])
            return address.is_private or address.is_loopback

        def do_POST(self):
            if not self.private_client():
                return self.reply(403, b'{"error":"private network only"}')
            if self.path != "/api/network/plan" or not planner:
                return self.denied()
            origin = self.headers.get("Origin")
            if (origin and urllib.parse.urlsplit(origin).netloc != self.headers.get("Host")) or self.headers.get("Sec-Fetch-Site") == "cross-site":
                return self.reply(403, b'{"error":"origin rejected"}')
            if self.headers.get_content_type() != "application/json" or self.headers.get("Transfer-Encoding"):
                return self.reply(400, b'{"error":"JSON required"}')
            try:
                size = int(self.headers.get("Content-Length", "0"))
                if size < 1 or size > 1024 * 1024:
                    raise ValueError("request limit")
                raw = self.rfile.read(size)
                data = json.loads(raw)
                if not isinstance(data, dict) or data.get("kind") != "firewall":
                    return self.denied()
                request = urllib.request.Request(planner + "/api/network/plan", data=raw, headers={"Content-Type":"application/json"}, method="POST")
                with opener.open(request, timeout=60) as response:
                    raw = response.read(32 * 1024 * 1024 + 1)
                if len(raw) > 32 * 1024 * 1024:
                    raise ValueError("response limit")
                result = json.loads(raw)
                if not isinstance(result, dict):
                    raise ValueError("invalid plan")
                result["preview_only"] = True
                result.setdefault("warnings", []).insert(0, "只读预览：仅生成计划，未执行任何防火墙变更。")
                return self.reply(200, json.dumps(result, ensure_ascii=False).encode())
            except urllib.error.HTTPError as error:
                # The isolated planner only returns validation errors; never forward redirects.
                if error.code in (400, 409):
                    raw = error.read(65536)
                    try:
                        result = json.loads(raw)
                        message = result.get("error", "计划生成失败")
                    except (ValueError, AttributeError):
                        message = "计划生成失败"
                    return self.reply(error.code, json.dumps({"error":str(message)}, ensure_ascii=False).encode())
                return self.reply(502, b'{"error":"planner unavailable"}')
            except (OSError, ValueError, TypeError):
                return self.reply(400, b'{"error":"invalid or unavailable preview plan"}')

        do_PUT = do_PATCH = do_DELETE = do_OPTIONS = do_TRACE = do_CONNECT = denied

        def do_HEAD(self):
            self.do_GET()

        def do_GET(self):
            address = ipaddress.ip_address(self.client_address[0])
            if not (address.is_private or address.is_loopback):
                return self.reply(403, b'{"error":"private network only"}')
            path = urllib.parse.urlsplit(self.path).path
            if path == "/preview/status":
                return self.reply(200, json.dumps({"version": VERSION, "read_only": True}).encode())
            if path.startswith("/api/"):
                if self.command != "GET":
                    return self.denied()
                try:
                    source = planner if planner and path == "/api/network/firewall" else upstream
                    request = urllib.request.Request(source + self.path, headers={"Accept": "application/json"})
                    with opener.open(request, timeout=20) as response:
                        raw = response.read(32 * 1024 * 1024 + 1)
                    if len(raw) > 32 * 1024 * 1024:
                        raise ValueError("response limit")
                    data = json.loads(raw)
                    if path == "/api/state":
                        data["read_only"] = True
                        data["preview"] = {"version": VERSION, "server_changes": False}
                    elif path == "/api/health":
                        data["preview"] = VERSION
                    return self.reply(200, json.dumps(data, ensure_ascii=False).encode())
                except urllib.error.HTTPError as error:
                    return self.reply(error.code, b'{"error":"upstream request unavailable"}')
                except (OSError, ValueError):
                    return self.reply(502, b'{"error":"preview data unavailable"}')
            decoded = urllib.parse.unquote(path)
            if ".." in Path(decoded).parts or "\\" in decoded:
                return self.reply(404, b'{"error":"not found"}')
            file = (web / decoded.lstrip("/")).resolve()
            if not file.is_relative_to(web):
                return self.reply(404, b'{"error":"not found"}')
            if path == "/":
                file = web / "index.html"
            if not file.is_file():
                return self.reply(404, b'{"error":"not found"}')
            raw = file.read_bytes()
            if file.name == "index.html":
                marker = '<aside style="position:fixed;bottom:10px;left:10px;z-index:9999;padding:5px 10px;border-radius:6px;background:#163d35;color:#7de3ca;font:12px sans-serif;pointer-events:none">只读预览 · v' + VERSION + '</aside>'
                raw = raw.replace(b"</body>", marker.encode() + b"</body>")
            return self.reply(200, raw, mimetypes.guess_type(file.name)[0] or "application/octet-stream")

    return Preview


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--web", type=Path, required=True)
    parser.add_argument("--listen", default="127.0.0.1:18881")
    parser.add_argument("--upstream", default="http://127.0.0.1:18880")
    parser.add_argument("--planner", default="", help="optional isolated read-only planner HTTP origin on loopback")
    args = parser.parse_args()
    upstream = urllib.parse.urlsplit(args.upstream)
    if upstream.scheme != "http" or upstream.username or upstream.password or upstream.path not in ("", "/") or upstream.query or upstream.fragment:
        parser.error("upstream must be a plain private HTTP origin")
    try:
        ip = ipaddress.ip_address(upstream.hostname)
        if not (ip.is_private or ip.is_loopback):
            raise ValueError()
    except ValueError:
        parser.error("upstream must use a private or loopback IP")
    if args.planner:
        plan_origin = urllib.parse.urlsplit(args.planner)
        try:
            if plan_origin.scheme != "http" or plan_origin.username or plan_origin.password or plan_origin.path not in ("", "/") or plan_origin.query or plan_origin.fragment or not ipaddress.ip_address(plan_origin.hostname).is_loopback:
                raise ValueError()
        except (TypeError, ValueError):
            parser.error("planner must be a plain loopback HTTP origin")
    if not (args.web / "index.html").is_file():
        parser.error("frontend index.html missing")
    host, port = args.listen.rsplit(":", 1)
    ThreadingHTTPServer((host, int(port)), handler(args.web, args.upstream.rstrip("/"), args.planner.rstrip("/") or None)).serve_forever()


if __name__ == "__main__":
    main()
