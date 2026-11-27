"""FoXray Gateway 0.9.74 management-only deployment; see upgrade_v0.9.74_README.md."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import platform
import subprocess
import time
import urllib.request
import zipfile

VERSION = "0.9.74"

def digest(value):
    return hashlib.sha256(value).hexdigest()

def fingerprint(value):
    return digest(json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode())

def verify_archive(raw, expected):
    if digest(raw) != expected:
        raise ValueError("archive SHA256 mismatch")
    result = {}
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        total = 0
        if len(archive.infolist()) > 3000:
            raise ValueError("too many archive entries")
        for item in archive.infolist():
            name = item.filename
            path = PurePosixPath(name)
            mode = item.external_attr >> 16
            if (path.is_absolute() or ".." in path.parts or "\\" in name or ":" in name
                    or path.as_posix() != name.rstrip("/") or mode & 0o170000 == 0o120000):
                raise ValueError("unsafe archive member")
            if item.is_dir():
                continue
            if name in result:
                raise ValueError("duplicate archive member")
            total += item.file_size
            if total > 256 * 1024 * 1024 or item.file_size > 100 * 1024 * 1024:
                raise ValueError("archive too large")
            result[name] = archive.read(item)
    manifest = json.loads(result.get("MANIFEST_SHA256.json", b"{}"))
    if not manifest or set(manifest) != set(result) - {"MANIFEST_SHA256.json"}:
        raise ValueError("manifest does not match package")
    if any(digest(result[name]) != expected_hash for name, expected_hash in manifest.items()):
        raise ValueError("member SHA256 mismatch")
    if not result.get("foxray_v" + VERSION, b"").startswith(b"\x7fELF") or not result.get("web/index.html"):
        raise ValueError("missing management binary or frontend")
    return result

def command(*args):
    return subprocess.check_output(args, text=True, timeout=25).strip()

def get_json(base, path):
    with urllib.request.urlopen(base + path, timeout=25) as response:
        return json.load(response)

def snapshot(health):
    base = health.removesuffix("/api/health")
    state = get_json(base, "/api/state")
    selected = {key: state.get(key) for key in
                ["config", "native", "manager_settings", "icmp_policy", "components", "monitoring_items", "proxy"]}
    # Canonicalize only newly introduced defaults, preserving explicit choices.
    # Adding keys in 0.9.4 does not itself apply DNS interception or reload Xray.
    if isinstance(selected.get("config"), dict):
        configuration = dict(selected["config"])
        defaults = {"dns_direct_servers": ["114.114.114.114", "114.114.115.115", "223.5.5.5", "223.6.6.6", "119.29.29.29"],
                    "dns_proxy_servers": ["1.1.1.1", "8.8.8.8"], "lan_dns_intercept": True,
                    "dns_force_proxy": False, "domain_identification": False}
        for key, value in defaults.items():
            configuration.setdefault(key, value)
        selected["config"] = configuration
    # Version 0.9.1 omits this display-only default from its public snapshot.
    settings = selected.get("manager_settings")
    if isinstance(settings, dict):
        settings = dict(settings)
        presentation = dict(settings.get("presentation") or {})
        presentation.setdefault("home_header_auto_hide", False)
        settings["presentation"] = presentation
        selected["manager_settings"] = settings
    selected["exit"] = {key: state.get("state", {}).get(key) for key in ["active", "applied", "active_since"]}
    selected["host_policy"] = get_json(base, "/api/system").get("policy", {})
    routes = get_json(base, "/api/network/routes")
    rules = get_json(base, "/api/network/rules")
    links = get_json(base, "/api/network/interfaces")
    if not all(row.get("complete") for row in [routes, rules, links]):
        raise ValueError("network baseline incomplete")
    def ordered(items):
        return sorted(items, key=fingerprint)
    main = ordered([{key: row.get(key) for key in ["dst", "gateway", "dev", "table", "metric", "protocol", "prefsrc", "type"]}
                    for row in routes.get("ipv4", []) if row.get("table", "main") in ["main", 254]])
    interfaces = ordered([{key: row.get(key) for key in ["ifname", "flags", "mtu", "operstate"]}
                          for row in links.get("items", []) if row.get("ifname") != "lo"])
    dns = [{key: get_json(base, "/api/network/dns?scope=" + scope).get(key) for key in ["servers", "search"]}
           for scope in ["host", "warp"]]
    services = get_json(base, "/api/services").get("services", {})
    return {"configuration": fingerprint(selected), "configuration_categories": {k: fingerprint(v) for k, v in selected.items()},
            "core_pid": services.get("xray", {}).get("pid"), "warp_pid": services.get("warp", {}).get("pid"),
            "interfaces": fingerprint(interfaces), "main_routes": fingerprint(main),
            "policy_rules": fingerprint({k: rules.get(k) for k in ["ipv4", "ipv6"]}), "dns": fingerprint(dns)}

def switch_link(target, path):
    temporary = path.with_name(path.name + ".migration-link")
    if temporary.exists() or temporary.is_symlink():
        temporary.unlink()
    temporary.symlink_to(target)
    temporary.replace(path)

def healthy(address, expected):
    for _ in range(30):
        try:
            row = get_json(address.removesuffix("/api/health"), "/api/health")
            if row.get("ok") and row.get("version") == expected:
                return True
        except Exception:
            pass
        time.sleep(1)
    return False

def write_record(path, record):
    path.write_text(json.dumps(record, ensure_ascii=False, indent=2), encoding="utf8")
    path.chmod(0o600)

def ensure_public_rules(base, files):
    """Install the bundled public domain dataset only when absent."""
    raw = files.get("rules/gfwlist_domains.json")
    if raw is None:
        raise ValueError("package lacks public domain dataset")
    data = json.loads(raw)
    if not isinstance(data, dict) or not all(isinstance(data.get(k), list) for k in ["direct", "proxy"]):
        raise ValueError("invalid public domain dataset")
    directory = base / "rules"
    if directory.is_symlink():
        raise ValueError("rules directory must not be a symlink")
    directory.mkdir(mode=0o755, exist_ok=True)
    target = directory / "gfwlist_domains.json"
    if target.is_symlink():
        raise ValueError("public domain dataset must not be a symlink")
    try:
        with target.open("xb") as stream:
            stream.write(raw)
        target.chmod(0o644)
    except FileExistsError:
        pass

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path)
    parser.add_argument("--sha256")
    parser.add_argument("--base", type=Path, default=Path("/opt/foxray"))
    parser.add_argument("--health", default="http://127.0.0.1:18880/api/health")
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--rollback", type=Path)
    args = parser.parse_args()
    if platform.system() != "Linux" or platform.machine() not in ["x86_64", "amd64"]:
        parser.error("migration requires Linux amd64")
    if not args.health.startswith("http://") or not args.health.endswith("/api/health"):
        parser.error("health must be the existing trusted local HTTP health endpoint")
    base = args.base.resolve()
    binary, web = base / "foxray", base / "web"
    if not binary.is_symlink() or not web.is_symlink():
        parser.error("existing installation must use versioned binary and web symlinks")
    previous_binary, previous_web = binary.resolve(), web.resolve()
    if not all(path.is_relative_to(base) for path in [previous_binary, previous_web]):
        parser.error("existing symlinks must stay inside base")
    if args.rollback:
        if not args.apply or os.geteuid() != 0:
            parser.error("rollback requires root and --apply")
        record = json.loads(args.rollback.read_text(encoding="utf8"))
        if record.get("version") != VERSION or record.get("base") != str(base):
            parser.error("rollback record does not match installation")
        destinations = [Path(record[k]).resolve() for k in ["previous_binary", "previous_web"]]
        if not all(path.exists() and path.is_relative_to(base) for path in destinations):
            parser.error("rollback targets unavailable")
        switch_link(destinations[0], binary); switch_link(destinations[1], web)
        command("systemctl", "restart", "foxray.service")
        if not healthy(args.health, record["previous_version"]):
            raise RuntimeError("rollback health check failed")
        print(json.dumps({"rolled_back": True, "version": record["previous_version"]}))
        return
    if not args.archive or not args.sha256:
        parser.error("--archive and --sha256 are required")
    files = verify_archive(args.archive.read_bytes(), args.sha256)
    current = get_json(args.health.removesuffix("/api/health"), "/api/health")["version"]
    print(json.dumps({"verified": True, "current": current, "target": VERSION, "apply": args.apply}))
    if not args.apply:
        return
    if os.geteuid() != 0:
        parser.error("--apply requires root")
    if not current or not isinstance(current, str):
        parser.error("current management version is unavailable")
    pending = base / "private/manager-upgrade-plan.json"
    if pending.exists():
        parser.error("a management update plan is pending")
    before = snapshot(args.health)
    private = base / "private"; private.mkdir(mode=0o700, exist_ok=True)
    stamp = time.strftime("%Y%m%d-%H%M%S")
    stage = base / ("releases/v" + VERSION + "-migration-" + stamp)
    stage.mkdir(parents=True, exist_ok=False)
    for name, data in files.items():
        if name != "foxray_v" + VERSION and not name.startswith("web/"):
            continue
        path = stage / name; path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data); path.chmod(0o755 if name == "foxray_v" + VERSION else 0o644)
    record = {"version": VERSION, "base": str(base), "previous_version": current,
              "previous_binary": str(previous_binary), "previous_web": str(previous_web),
              "new_binary": str(stage / ("foxray_v" + VERSION)), "new_web": str(stage / "web"),
              "package_sha256": args.sha256, "baseline": before}
    record_path = private / ("migration-v" + VERSION + "-" + stamp + ".json")
    write_record(record_path, record)
    try:
        ensure_public_rules(base, files)
        switch_link(record["new_binary"], binary); switch_link(record["new_web"], web)
        command("systemctl", "restart", "foxray.service")
        if not healthy(args.health, VERSION):
            raise RuntimeError("new management service failed health check")
        after = snapshot(args.health)
        changed = [key for key in before if key != "configuration_categories" and before[key] != after[key]]
        if changed:
            record["changed_categories"] = changed
            raise RuntimeError("post-update baseline changed: " + ", ".join(changed))
        record["verified"] = True; record["after"] = after
        write_record(record_path, record)
    except Exception:
        switch_link(previous_binary, binary); switch_link(previous_web, web)
        command("systemctl", "restart", "foxray.service")
        record["rolled_back"] = healthy(args.health, current)
        write_record(record_path, record)
        raise
    print(json.dumps({"updated": True, "version": VERSION, "baseline_unchanged": True, "record": str(record_path)}))

if __name__ == "__main__":
    main()
