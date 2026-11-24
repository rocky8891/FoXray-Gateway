"""Build FoXray Gateway v0.9.17; see build_release_v0.9.17_README.md."""
import argparse
import hashlib
import io
import json
import re
from pathlib import Path
import shutil
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parent
VERSION = "0.9.17"


def command(*args):
    return subprocess.check_output(args, cwd=ROOT)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def write_zip(path, files):
    manifest = {name: digest(raw) for name, raw in files.items()}
    files = dict(files)
    files["MANIFEST_SHA256.json"] = json.dumps(manifest, indent=2, sort_keys=True).encode()
    path.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for name, raw in sorted(files.items()):
            info = zipfile.ZipInfo(name)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = (0o100755 if name.endswith(".sh") or name == "foxray_v" + VERSION else 0o100644) << 16
            archive.writestr(info, raw)
    with zipfile.ZipFile(path) as archive:
        assert set(archive.namelist()) == set(files)
        for name, expected in manifest.items():
            if digest(archive.read(name)) != expected:
                raise ValueError("package digest differs: " + name)
    return path


def license_files(go):
    result = {}
    node = ROOT / "frontend/node_modules"
    for path in node.rglob("*"):
        if path.is_file() and path.name.lower() in {"license", "license.txt", "license.md", "copying", "notice", "notice.txt"}:
            result["third_party/licenses/npm/" + path.relative_to(node).as_posix()] = path.read_bytes()
    metadata = command(go, "-C", "backend", "list", "-m", "-json", "all").decode()
    decoder = json.JSONDecoder()
    while metadata.strip():
        metadata = metadata.lstrip()
        module, offset = decoder.raw_decode(metadata)
        metadata = metadata[offset:]
        if module.get("Main") or not module.get("Dir"):
            continue
        directory = Path(module["Dir"])
        for pattern in ["LICENSE*", "COPYING*", "NOTICE*"]:
            for path in directory.glob(pattern):
                if path.is_file():
                    result["third_party/licenses/go/" + module["Path"] + "/" + path.name] = path.read_bytes()
    goroot = Path(command(go, "env", "GOROOT").decode().strip())
    result["third_party/licenses/go-toolchain/LICENSE"] = (goroot / "LICENSE").read_bytes()
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default=shutil.which("go"))
    args = parser.parse_args()
    if not args.go:
        parser.error("Go executable required")
    if command("git", "status", "--porcelain", "--", ".", ":(exclude)assets").strip():
        raise ValueError("commit source changes before packaging")
    package = json.loads((ROOT / "frontend/package.json").read_text(encoding="utf8"))
    lock = json.loads((ROOT / "frontend/package-lock.json").read_text(encoding="utf8"))
    if any(value != VERSION for value in [package["version"], lock["version"], lock["packages"][""]["version"]]):
        raise ValueError("frontend version mismatch")
    if f'const Version = "{VERSION}"' not in (ROOT / "backend/internal/gateway/util.go").read_text(encoding="utf8"):
        raise ValueError("backend version mismatch")
    for name, expected in lock["packages"].items():
        installed = ROOT / "frontend" / name / "package.json"
        if name and expected.get("version") and installed.is_file():
            if json.loads(installed.read_text(encoding="utf8")).get("version") != expected["version"]:
                raise ValueError("installed dependency differs: " + name)
    binary = (ROOT / ("output/foxray_v" + VERSION)).read_bytes()
    if binary[:6] != b"\x7fELF\x02\x01" or int.from_bytes(binary[18:20], "little") != 62:
        raise ValueError("expected Linux amd64 ELF executable")
    files = {"foxray_v" + VERSION: binary}
    with zipfile.ZipFile(io.BytesIO(command("git", "archive", "--format=zip", "HEAD"))) as archive:
        source = {name: archive.read(name) for name in archive.namelist() if not name.endswith("/")}
    for name, raw in source.items():
        if re.search(r"0\.9\.(?:[1-9]|1[0-6])(?![0-9])", name):
            continue
        if name.endswith(".md") or name == "LICENSE" or name.startswith(("docs/", "release-notes/", "rules/", "third_party/")) or name in ["install_v0.9.17.sh", "upgrade_v0.9.17.py", "prepare_offline_v0.9.17.sh"]:
            files[name] = raw
    for path in (ROOT / "frontend/dist").rglob("*"):
        if path.is_file():
            files["web/" + path.relative_to(ROOT / "frontend/dist").as_posix()] = path.read_bytes()
    if "web/index.html" not in files:
        raise ValueError("build frontend before packaging")
    files.update(license_files(args.go))
    paths = [write_zip(ROOT / f"output/FoXray-Gateway-v{VERSION}-linux-amd64.zip", files),
             write_zip(ROOT / f"output/FoXray-Gateway-v{VERSION}-source.zip", source)]
    sums = "".join(digest(path.read_bytes()) + "  " + path.name + "\n" for path in paths)
    (ROOT / "output/SHA256SUMS.txt").write_text(sums, encoding="utf8", newline="\n")
    print(json.dumps({"version": VERSION, "runtime_files": len(files), "source_files": len(source), "verified": True}))


if __name__ == "__main__":
    main()
