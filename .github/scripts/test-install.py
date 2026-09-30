"""Exercise the installer against a local release server with no Go on PATH."""

import hashlib
import http.server
import io
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tarfile
import tempfile
import threading
import unittest

ROOT = Path(__file__).resolve().parents[2]
BINARY = Path(sys.argv.pop(1)).resolve()
ARCH = {"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[platform.machine()]
NAME = f"sdash_0.1.0_linux_{ARCH}.tar.gz"
COMPLETION = subprocess.check_output([str(BINARY), "completion", "bash"])
PAYLOAD = BINARY.read_bytes()
buf = io.BytesIO()
with tarfile.open(fileobj=buf, mode="w:gz") as archive:
    for name, data in {"sdash": PAYLOAD, "completions/sdash.bash": COMPLETION,
                       "LICENSE": (ROOT / "LICENSE").read_bytes(),
                       "README.md": (ROOT / "README.md").read_bytes()}.items():
        entry = tarfile.TarInfo(name)
        entry.size, entry.mode = len(data), 0o755 if name == "sdash" else 0o644
        archive.addfile(entry, io.BytesIO(data))
ARCHIVE = buf.getvalue()
if release_archive := os.environ.get("SDASH_PACKAGING_ARCHIVE"):
    ARCHIVE = Path(release_archive).read_bytes()
    with tarfile.open(fileobj=io.BytesIO(ARCHIVE), mode="r:gz") as archive:
        assert archive.extractfile("sdash").read() == PAYLOAD
SUMS = f"{hashlib.sha256(ARCHIVE).hexdigest()}  {NAME}\n".encode()


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_GET(self):
        files = {"/api/releases/latest": b'{"tag_name":"v0.1.0"}',
                 f"/dl/v0.1.0/{NAME}": ARCHIVE,
                 "/dl/v0.1.0/checksums.txt": SUMS,
                 "/bad/v0.1.0/checksums.txt": b"0" * 64 + f"  {NAME}\n".encode(),
                 f"/bad/v0.1.0/{NAME}": ARCHIVE}
        data = files.get(self.path)
        self.send_response(200 if data is not None else 404)
        self.end_headers()
        if data is not None:
            self.wfile.write(data)


class Installer(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.url = f"http://127.0.0.1:{cls.server.server_port}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="sdash-package-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home, self.tmp, self.tools = [self.root / p for p in ("home", "tmp", "tools")]
        for directory in (self.home, self.tmp, self.tools):
            directory.mkdir()
        for tool in ("uname", "curl", "sha256sum", "cut", "mktemp", "rm", "sed", "head",
                     "awk", "mkdir", "tar", "gzip", "chmod", "mv", "wc", "tr", "cat"):
            (self.tools / tool).symlink_to(shutil.which(tool))
        self.env = dict(os.environ, HOME=str(self.home), TMPDIR=str(self.tmp),
                        PATH=str(self.tools), XDG_DATA_HOME=str(self.home / "data"),
                        SDASH_API_URL=self.url + "/api", SDASH_DOWNLOAD_URL=self.url + "/dl")
        self.env.pop("SDASH_INSTALL_DIR", None)
        for key in ("http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"):
            self.env.pop(key, None)
        self.env["NO_PROXY"] = "127.0.0.1"
        self.target = self.home / ".local/bin/sdash"
        self.metrics = self.root / "peak.json"
        # Observe the exact pre-rename peak, without relying on polling timing.
        self.stub("mv", f'''#!{sys.executable}
import json, os, pathlib, sys
files = [p for d in ({str(self.home)!r}, {str(self.tmp)!r})
         for p in pathlib.Path(d).rglob('*') if p.is_file()]
pathlib.Path({str(self.metrics)!r}).write_text(json.dumps({{
    'file_bytes': sum(p.stat().st_size for p in files),
    'allocated_bytes': sum(p.stat().st_blocks * 512 for p in files)}}))
os.execv({shutil.which('mv')!r}, ['mv'] + sys.argv[1:])
''')

    def stub(self, tool, body):
        dest = self.tools / tool
        dest.unlink(missing_ok=True)
        dest.write_text(body)
        dest.chmod(0o755)

    def old_binary(self):
        self.target.parent.mkdir(parents=True, exist_ok=True)
        self.target.write_bytes(PAYLOAD)
        self.target.chmod(0o755)

    def run_install(self, *args, success=True):
        result = subprocess.run(["/bin/sh", str(ROOT / "install.sh"), *args],
                                env=self.env, capture_output=True, text=True, timeout=45)
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        self.assertEqual(list(self.tmp.iterdir()), [], "download files leaked")
        self.assertEqual(list(self.home.rglob(".sdash-install.*")), [], "staged files leaked")
        return result

    def report(self, label):
        peak = json.loads(self.metrics.read_text())
        line = (f"{label} ({ARCH}): archive={len(ARCHIVE)} B; installed={len(PAYLOAD)} B; "
                f"peak file bytes={peak['file_bytes']} B; allocated={peak['allocated_bytes']} B")
        print(line, flush=True)
        if summary := os.environ.get("GITHUB_STEP_SUMMARY"):
            with open(summary, "a") as out:
                out.write(line + "\n\n")

    def test_clean_install_without_go(self):
        self.assertIsNone(shutil.which("go", path=self.env["PATH"]))
        result = self.run_install()
        self.assertIn("Installed size:", result.stdout)
        self.assertIn("not on your PATH", result.stdout)
        self.assertEqual([p for p in self.home.rglob("*") if p.is_file()], [self.target])
        self.assertEqual(self.target.read_bytes(), PAYLOAD)
        self.report("clean install")
        for args in (["version"], ["--demo", "status", "--json"]):
            subprocess.run([str(self.target), *args], env=self.env,
                           check=True, capture_output=True, timeout=30)

    def test_reinstall(self):
        self.old_binary()
        self.run_install("--version", "v0.1.0")
        self.assertEqual(self.target.read_bytes(), PAYLOAD)
        self.assertEqual(list(self.target.parent.iterdir()), [self.target])
        self.report("installer upgrade")

    def test_custom_pinned_completion(self):
        directory = self.home / "custom bin"
        self.run_install("--version=0.1.0", "--dir", str(directory), "--completions", "bash")
        self.assertEqual((directory / "sdash").read_bytes(), PAYLOAD)
        self.assertEqual((self.home / "data/bash-completion/completions/sdash").read_bytes(), COMPLETION)
        self.assertFalse(self.target.exists())

    def test_environment_directory(self):
        self.env["SDASH_INSTALL_DIR"] = str(self.home / "env-bin")
        self.run_install("--version", "v0.1.0")
        self.assertTrue((self.home / "env-bin/sdash").exists())

    def test_failures_preserve_binary(self):
        cases = {
            "checksum": (None, None),
            "unsupported platform": ("uname", "#!/bin/sh\necho unsupported\n"),
            "failed extraction": ("tar", "#!/bin/sh\nprintf partial\nexit 1\n"),
            "failed staging": ("chmod", "#!/bin/sh\nexit 1\n"),
            "failed replacement": ("mv", "#!/bin/sh\nexit 1\n"),
            "interrupted download": ("curl", '#!/bin/sh\nprintf partial > "$5"\nkill -TERM "$PPID"\nexit 1\n'),
            "interrupted staging": ("tar", '#!/bin/sh\nprintf partial\nkill -TERM "$PPID"\nexit 1\n'),
        }
        for name, (tool, body) in cases.items():
            with self.subTest(name=name):
                self.old_binary()
                previous = (self.tools / tool).read_bytes() if tool and not (self.tools / tool).is_symlink() else None
                link = os.readlink(self.tools / tool) if tool and (self.tools / tool).is_symlink() else None
                if tool:
                    self.stub(tool, body)
                else:
                    self.env["SDASH_DOWNLOAD_URL"] = self.url + "/bad"
                self.run_install("--version", "v0.1.0", success=False)
                self.assertEqual(self.target.read_bytes(), PAYLOAD)
                self.assertEqual(list(self.target.parent.iterdir()), [self.target])
                if tool:
                    (self.tools / tool).unlink()
                    if link:
                        (self.tools / tool).symlink_to(link)
                    else:
                        self.stub(tool, previous.decode())
                self.env["SDASH_DOWNLOAD_URL"] = self.url + "/dl"

    def test_unwritable_destination(self):
        if os.geteuid() == 0:
            self.skipTest("permission test needs an unprivileged user")
        self.old_binary()
        self.target.parent.chmod(0o555)
        try:
            self.run_install("--version", "v0.1.0", success=False)
            self.assertEqual(self.target.read_bytes(), PAYLOAD)
        finally:
            self.target.parent.chmod(0o755)

    def test_invalid_completion(self):
        self.run_install("--completions", "unknown", success=False)

    def test_size_gate_boundary(self):
        binary = self.root / "sparse-binary"
        completion = self.root / "completion"
        completion.write_bytes(b"x")
        for size, extra, success in [(20971519, False, True), (20971520, False, False),
                                      (20971521, False, False), (20971519, True, False)]:
            with binary.open("wb") as out:
                out.truncate(size)
            command = [sys.executable, str(ROOT / ".github/scripts/check-size.py"), str(binary)]
            if extra:
                command += ["--completion", str(completion)]
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
