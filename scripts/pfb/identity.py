"""Deterministic PFB identity and resource names."""

from __future__ import annotations

import hashlib
import re

from .errors import PFBError


PFB_ID = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,22}[a-z0-9])?$")
CORE_ID = re.compile(r"^[a-z0-9_]{1,64}$")
CONTROL_OR_SPACE = re.compile(r"[\x00-\x20\x7f-\x9f]")
NON_SLUG = re.compile(r"[^a-z0-9]+")


def pfb_id(name: str) -> str:
    try:
        encoded = name.encode("utf-8")
    except UnicodeEncodeError as exc:
        raise PFBError("PFB_SPEC_INVALID", "name") from exc
    if not 1 <= len(encoded) <= 128 or CONTROL_OR_SPACE.search(name):
        raise PFBError("PFB_SPEC_INVALID", "name")
    suffix = hashlib.sha256(encoded).hexdigest()[:12]
    slug = NON_SLUG.sub("-", name.lower()).strip("-") or "pfb"
    slug = slug[:11].rstrip("-") or "pfb"
    result = f"{slug}-{suffix}"
    if len(result) > 24 or PFB_ID.fullmatch(result) is None:
        raise PFBError("PFB_SPEC_INVALID", "id")
    return result


def validate_pfb_id(value: str) -> str:
    if not isinstance(value, str) or PFB_ID.fullmatch(value) is None:
        raise PFBError("PFB_SPEC_INVALID", "id")
    return value


def validate_core_id(value: str) -> str:
    if not isinstance(value, str) or CORE_ID.fullmatch(value) is None:
        raise PFBError("PFB_SPEC_INVALID", "core-id")
    return value


def compose_project(value: str) -> str:
    return f"retrom-pfb-{validate_pfb_id(value)}"


def network_alias(value: str) -> str:
    return compose_project(value)


def app_origin(value: str) -> str:
    return f"http://{validate_pfb_id(value)}.localhost:3000"


def runtime_origin_template(value: str) -> str:
    return f"http://{{launchId}}.{validate_pfb_id(value)}.rpg.localhost:3000"


def volume_name(value: str, role: str, digest: str) -> str:
    if role not in {"data", "node", "runtime-node", "go", "next", "npm"}:
        raise PFBError("PFB_SPEC_INVALID", "volume-role")
    if not isinstance(digest, str) or re.fullmatch(r"[0-9a-f]{64}", digest) is None:
        raise PFBError("PFB_SPEC_INVALID", "volume-digest")
    return f"{compose_project(value)}-{role}-{digest[:12]}"
