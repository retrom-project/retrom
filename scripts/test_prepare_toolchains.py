#!/usr/bin/env python3
"""Regression tests for atomic repository toolchain preparation."""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]


class ToolchainPreparationTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        (self.root / "scripts").mkdir()
        self.fake_bin = self.root / "fake-bin"
        self.fake_bin.mkdir()
        self._install_fake_download_commands()

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def write_executable(self, path: Path, source: str) -> None:
        path.write_text(textwrap.dedent(source).lstrip(), encoding="utf-8")
        path.chmod(0o755)

    def copy_script(self, name: str) -> Path:
        destination = self.root / "scripts" / name
        shutil.copy2(REPOSITORY_ROOT / "scripts" / name, destination)
        return destination

    def run_script(
        self, script: Path, *, fail_download: bool = False
    ) -> subprocess.CompletedProcess[str]:
        environment = os.environ.copy()
        environment["PATH"] = f"{self.fake_bin}:{environment['PATH']}"
        if fail_download:
            environment["RETROM_FAKE_DOWNLOAD_FAILURE"] = "1"
        return subprocess.run(
            [str(script)],
            cwd=self.root,
            env=environment,
            check=False,
            capture_output=True,
            text=True,
        )

    def _install_fake_download_commands(self) -> None:
        self.write_executable(
            self.fake_bin / "curl",
            r"""
            #!/usr/bin/env python3
            import os
            import pathlib
            import sys

            if os.environ.get("RETROM_FAKE_DOWNLOAD_FAILURE"):
                raise SystemExit(22)
            output = pathlib.Path(sys.argv[sys.argv.index("--output") + 1])
            if output.name == "SHASUMS256.txt":
                output.write_text(
                    "a" * 64 + "  node-v24.18.0-linux-x64.tar.xz\n",
                    encoding="utf-8",
                )
            else:
                output.write_bytes(b"verified fake archive")
            """,
        )
        self.write_executable(
            self.fake_bin / "sha256sum",
            r"""
            #!/usr/bin/env python3
            import pathlib
            import sys

            filename = pathlib.Path(sys.argv[-1]).name
            digest = (
                "5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053"
                if filename.startswith("go1.26.5")
                else "a" * 64
            )
            print(digest, sys.argv[-1])
            """,
        )
        self.write_executable(
            self.fake_bin / "tar",
            r"""
            #!/usr/bin/env python3
            import pathlib
            import sys

            destination = pathlib.Path(sys.argv[sys.argv.index("-C") + 1])
            archive = next(value for value in sys.argv if value.endswith((".tar.xz", ".tar.gz")))
            if archive.endswith(".tar.xz"):
                binary_root = destination / "node-v24.18.0-linux-x64" / "bin"
                commands = {"node": "v24.18.0", "npm": "11.16.0"}
                for name, version in commands.items():
                    target = binary_root / name
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_text(f"#!/bin/sh\necho {version}\n", encoding="utf-8")
                    target.chmod(0o755)
            else:
                target = destination / "go" / "bin" / "go"
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text("#!/bin/sh\necho go1.26.5\n", encoding="utf-8")
                target.chmod(0o755)
            """,
        )

    def test_invalid_node_cache_is_restored_on_failure_then_rebuilt(self) -> None:
        script = self.copy_script("prepare-node.sh")
        target = self.root / ".cache/tools/node-v24.18.0-linux-x64"
        target.mkdir(parents=True)
        marker = target / "corrupt-marker"
        marker.write_text("preserve until replacement succeeds", encoding="utf-8")

        failed = self.run_script(script, fail_download=True)
        self.assertNotEqual(failed.returncode, 0, failed.stderr)
        self.assertTrue(marker.is_file(), failed.stderr)

        rebuilt = self.run_script(script)
        self.assertEqual(rebuilt.returncode, 0, rebuilt.stderr)
        self.assertFalse(marker.exists())
        self.assertEqual(
            subprocess.check_output([target / "bin/node", "--version"], text=True).strip(),
            "v24.18.0",
        )

    def test_invalid_go_cache_is_atomically_rebuilt(self) -> None:
        (self.root / "go.mod").write_text("module example\n\ngo 1.26.5\n", encoding="utf-8")
        script = self.copy_script("prepare-go.sh")
        target = self.root / ".cache/tools/go1.26.5-linux-amd64"
        target.mkdir(parents=True)
        marker = target / "corrupt-marker"
        marker.write_text("invalid", encoding="utf-8")

        rebuilt = self.run_script(script)
        self.assertEqual(rebuilt.returncode, 0, rebuilt.stderr)
        self.assertFalse(marker.exists())
        self.assertEqual(
            subprocess.check_output([target / "bin/go", "env", "GOVERSION"], text=True).strip(),
            "go1.26.5",
        )


if __name__ == "__main__":
    unittest.main()
