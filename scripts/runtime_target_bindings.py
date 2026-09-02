from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any


ROOT_KEYS = {"schemaVersion", "catalogVersion", "bindings"}
BINDING_KEYS = {
    "id", "coreId", "providerId", "targetId", "platformIds", "acceptedContentKinds",
    "detectorProfile", "deliveryProfile", "launchPolicy", "reviewPolicy",
}
KEBAB = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
IDENTIFIER = re.compile(r"^[a-z0-9]+(?:[_-][a-z0-9]+)*$")
PROFILE = re.compile(r"^[A-Z][A-Z0-9_]{1,63}$")
LAUNCH_POLICIES = {"SUPPORTED", "EXPERIMENTAL", "DISABLED"}
REVIEW_POLICIES = {"NONE", "RPG_RUNTIME_VALIDATION_V1"}


def load_runtime_target_bindings(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ValueError("RUNTIME_TARGET_BINDINGS_INVALID") from error
    validate_runtime_target_bindings(value)
    return value


def validate_runtime_target_bindings(value: Any) -> None:
    if not isinstance(value, dict) or set(value) != ROOT_KEYS:
        _invalid()
    if value["schemaVersion"] != 1 or not _positive_integer(value["catalogVersion"]):
        _invalid()
    bindings = value["bindings"]
    if not isinstance(bindings, list) or not bindings:
        _invalid()
    identities: list[tuple[str, str]] = []
    binding_ids: set[str] = set()
    for binding in bindings:
        if not isinstance(binding, dict) or set(binding) != BINDING_KEYS:
            _invalid()
        binding_id = _matched(binding["id"], KEBAB)
        if binding_id in binding_ids:
            _invalid()
        binding_ids.add(binding_id)
        _matched(binding["coreId"], IDENTIFIER)
        provider_id = _matched(binding["providerId"], KEBAB)
        target_id = _matched(binding["targetId"], IDENTIFIER)
        platform_ids = _string_set(binding["platformIds"], IDENTIFIER)
        _string_set(binding["acceptedContentKinds"], PROFILE)
        _matched(binding["detectorProfile"], PROFILE)
        _matched(binding["deliveryProfile"], PROFILE)
        if binding["launchPolicy"] not in LAUNCH_POLICIES or binding["reviewPolicy"] not in REVIEW_POLICIES:
            _invalid()
        identities.append((provider_id, target_id))
    if identities != sorted(identities, key=lambda item: (item[0].encode(), item[1].encode())):
        _invalid()
    if len(set(identities)) != len(identities):
        _invalid()


def _string_set(value: Any, pattern: re.Pattern[str]) -> list[str]:
    if not isinstance(value, list) or not value or any(not isinstance(item, str) for item in value):
        _invalid()
    if any(pattern.fullmatch(item) is None for item in value):
        _invalid()
    if value != sorted(value, key=lambda item: item.encode()) or len(set(value)) != len(value):
        _invalid()
    return value


def _matched(value: Any, pattern: re.Pattern[str]) -> str:
    if not isinstance(value, str) or pattern.fullmatch(value) is None:
        _invalid()
    return value


def _positive_integer(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value > 0


def _invalid() -> None:
    raise ValueError("RUNTIME_TARGET_BINDINGS_INVALID")
