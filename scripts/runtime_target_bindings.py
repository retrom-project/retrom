from __future__ import annotations

import json
import re
import unicodedata
from pathlib import Path
from typing import Any


ROOT_KEYS = {"schemaVersion", "bindings", "definitions"}
BINDING_KEYS = {
    "id", "coreId", "providerId", "targetId", "platformIds", "acceptedContentKinds",
    "detectorProfile", "deliveryProfile", "launchPolicy", "reviewPolicy",
}
KEBAB = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
IDENTIFIER = re.compile(r"^[a-z0-9]+(?:[_-][a-z0-9]+)*$")
PROFILE = re.compile(r"^[A-Z][A-Z0-9_]{1,63}$")
VERSIONED_SEMANTIC = re.compile(r"_V[0-9]+$")
LAUNCH_POLICIES = {"SUPPORTED", "EXPERIMENTAL", "DISABLED"}
REVIEW_POLICIES = {"NONE", "RPG_RUNTIME_VALIDATION"}


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
    if value["schemaVersion"] != 1:
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
        content_kinds = _string_set(binding["acceptedContentKinds"], PROFILE)
        detector_profile = _matched(binding["detectorProfile"], PROFILE)
        delivery_profile = _matched(binding["deliveryProfile"], PROFILE)
        if binding["launchPolicy"] not in LAUNCH_POLICIES or binding["reviewPolicy"] not in REVIEW_POLICIES:
            _invalid()
        if any(VERSIONED_SEMANTIC.search(item) for item in (
            *content_kinds, detector_profile, delivery_profile,
            binding["launchPolicy"], binding["reviewPolicy"],
        )):
            _invalid()
        identities.append((provider_id, target_id))
    if identities != sorted(identities, key=lambda item: (item[0].encode(), item[1].encode())):
        _invalid()
    if len(set(identities)) != len(identities):
        _invalid()
    _validate_definitions(value["definitions"], bindings)


def _validate_definitions(value: Any, bindings: list[dict[str, Any]]) -> None:
    if not isinstance(value, dict) or set(value) != {"platforms", "cores", "contentKinds", "assetPacks"}:
        _invalid()
    platforms = _definition_rows(value["platforms"], {"id", "name", "sortOrder", "enabled"})
    cores = _definition_rows(value["cores"], {"id", "name", "enabled"})
    kinds = _string_set(value["contentKinds"], PROFILE)
    for binding in bindings:
        if binding["coreId"] not in cores or not set(binding["platformIds"]).issubset(platforms):
            _invalid()
        if not set(binding["acceptedContentKinds"]).issubset(kinds):
            _invalid()
    packs = value["assetPacks"]
    if not isinstance(packs, list):
        _invalid()
    keys = {"id", "kind", "generation", "declaredName", "normalizedDeclaredName", "displayName", "requiredLayoutVersion", "enabled"}
    previous = ""
    identities = set()
    for pack in packs:
        if not isinstance(pack, dict) or set(pack) != keys:
            _invalid()
        pack_id = _matched(pack["id"], IDENTIFIER)
        _matched(pack["generation"], PROFILE)
        _matched(pack["requiredLayoutVersion"], IDENTIFIER)
        if not isinstance(pack["kind"], str) or not re.fullmatch(r"[A-Za-z0-9_]{2,64}", pack["kind"]):
            _invalid()
        if not _name(pack["displayName"], 200) or not _name(pack["declaredName"], 512):
            _invalid()
        normalized = unicodedata.normalize("NFKC", pack["declaredName"].strip()).casefold()
        if pack["normalizedDeclaredName"] != normalized or type(pack["enabled"]) is not bool:
            _invalid()
        identity = (pack["generation"], pack["normalizedDeclaredName"])
        if pack_id <= previous or identity in identities:
            _invalid()
        identities.add(identity)
        previous = pack_id


def _definition_rows(value: Any, keys: set[str]) -> set[str]:
    if not isinstance(value, list) or not value:
        _invalid()
    ids = []
    for row in value:
        if not isinstance(row, dict) or set(row) != keys:
            _invalid()
        ids.append(_matched(row["id"], IDENTIFIER))
        if not _name(row["name"], 200) or type(row["enabled"]) is not bool:
            _invalid()
        if "sortOrder" in keys and (type(row["sortOrder"]) is not int or row["sortOrder"] < 0):
            _invalid()
    if ids != sorted(ids) or len(ids) != len(set(ids)):
        _invalid()
    return set(ids)


def _name(value: Any, maximum: int) -> bool:
    return isinstance(value, str) and 1 <= len(value.encode()) <= maximum and value == value.strip() and "\0" not in value


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
