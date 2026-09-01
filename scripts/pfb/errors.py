"""Stable PFB command failures."""

from __future__ import annotations


class PFBError(RuntimeError):
    """A user-visible failure with a stable machine code."""

    def __init__(self, code: str, detail: str = "") -> None:
        super().__init__(f"{code}{':' + detail if detail else ''}")
        self.code = code
        self.detail = detail
