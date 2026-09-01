#!/usr/bin/env python3
"""Tests for the local-development user boundary."""

from __future__ import annotations

import os
import subprocess
import sys
import unittest
from pathlib import Path

from local_user import ERROR, LocalUserError, require_local_user


ROOT = Path(__file__).resolve().parents[1]


class LocalUserTests(unittest.TestCase):
    def test_normal_user_identity_is_returned(self) -> None:
        self.assertEqual(
            (1234, 5678),
            require_local_user(
                environ={}, geteuid=lambda: 1234, getuid=lambda: 1234, getgid=lambda: 5678,
            ),
        )

    def test_root_real_or_effective_uid_is_rejected(self) -> None:
        for real_uid, effective_uid in ((0, 0), (0, 1234), (1234, 0)):
            with self.subTest(real_uid=real_uid, effective_uid=effective_uid):
                with self.assertRaisesRegex(LocalUserError, "LOCAL_DEVELOPMENT_ROOT_FORBIDDEN"):
                    require_local_user(
                        environ={}, geteuid=lambda: effective_uid,
                        getuid=lambda: real_uid, getgid=lambda: 1234,
                    )

    def test_every_sudo_marker_is_rejected(self) -> None:
        for key in ("SUDO_COMMAND", "SUDO_GID", "SUDO_UID", "SUDO_USER"):
            with self.subTest(key=key), self.assertRaisesRegex(
                LocalUserError, "LOCAL_DEVELOPMENT_ROOT_FORBIDDEN",
            ):
                require_local_user(
                    environ={key: "present"}, geteuid=lambda: 1234,
                    getuid=lambda: 1234, getgid=lambda: 1234,
                )

    def test_direct_pfb_cli_rejects_sudo_before_argument_parsing(self) -> None:
        environment = {**os.environ, "SUDO_USER": "test-user"}
        result = subprocess.run(
            [sys.executable, "-m", "scripts.pfb.cli", "--help"], cwd=ROOT,
            env=environment, capture_output=True, text=True, check=False,
        )
        self.assertEqual(2, result.returncode)
        self.assertEqual(ERROR + "\n", result.stderr)

    def test_direct_dev_script_rejects_sudo_before_touching_state(self) -> None:
        environment = {**os.environ, "SUDO_USER": "test-user"}
        result = subprocess.run(
            [str(ROOT / "scripts/dev.sh"), "--stop"], cwd=ROOT,
            env=environment, capture_output=True, text=True, check=False,
        )
        self.assertEqual(2, result.returncode)
        self.assertEqual(ERROR + "\n", result.stderr)


if __name__ == "__main__":
    unittest.main()
