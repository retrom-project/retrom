#!/usr/bin/env python3
"""Reject privileged local-development invocations."""

from __future__ import annotations

import os
import sys
from collections.abc import Callable, Mapping


ERROR = (
    "LOCAL_DEVELOPMENT_ROOT_FORBIDDEN: root/sudo is not allowed; "
    "run make dev or make pfb-* as your normal user"
)
SUDO_ENVIRONMENT_KEYS = ("SUDO_COMMAND", "SUDO_GID", "SUDO_UID", "SUDO_USER")


class LocalUserError(RuntimeError):
    """The caller is root or was launched through sudo."""


def require_local_user(
    *,
    environ: Mapping[str, str] | None = None,
    geteuid: Callable[[], int] = os.geteuid,
    getuid: Callable[[], int] = os.getuid,
    getgid: Callable[[], int] = os.getgid,
) -> tuple[int, int]:
    environment = os.environ if environ is None else environ
    uid = getuid()
    if uid == 0 or geteuid() == 0 or any(environment.get(key) for key in SUDO_ENVIRONMENT_KEYS):
        raise LocalUserError(ERROR)
    return uid, getgid()


def main() -> int:
    try:
        require_local_user()
    except LocalUserError as exc:
        print(str(exc), file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
