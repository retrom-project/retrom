"""Shared layout validation and DAT serialization for CPS fixtures."""

from __future__ import annotations

import binascii
import hashlib
from typing import Any
from xml.etree import ElementTree

from crc32_patch import force_crc32


def materialize_entries(
    driver: dict[str, Any],
    sources: dict[str, bytes],
) -> dict[str, bytes]:
    entries: dict[str, bytes] = {}
    for specification in driver["entries"]:
        name = specification["name"]
        content = sources.get(name, bytes(specification["size"]))
        if len(content) != specification["size"]:
            raise ValueError(f"invalid generated size for {name}")
        patch_offset = specification["patchOffset"]
        if patch_offset < 0x500 and specification["region"] in {"68000", "z80"}:
            raise ValueError(f"CRC patch overlaps executable fixture payload: {name}")
        content = force_crc32(
            content,
            int(specification["crc32"], 16),
            patch_offset,
        )
        if binascii.crc32(content) & 0xFFFFFFFF != int(specification["crc32"], 16):
            raise ValueError(f"invalid generated CRC32 for {name}")
        entries[name] = content
    if set(entries) != {entry["name"] for entry in driver["entries"]}:
        raise ValueError("generated CPS entries do not exactly match the driver layout")
    return entries


def _append_machine(
    root: ElementTree.Element,
    driver_name: str,
    description: str,
    entries: dict[str, bytes],
    attributes: dict[str, str] | None = None,
) -> None:
    machine_attributes = {"name": driver_name}
    machine_attributes.update(attributes or {})
    machine = ElementTree.SubElement(root, "game", machine_attributes)
    ElementTree.SubElement(machine, "description").text = description
    for name, content in entries.items():
        ElementTree.SubElement(
            machine,
            "rom",
            {
                "name": name,
                "size": str(len(content)),
                "crc": f"{binascii.crc32(content) & 0xFFFFFFFF:08x}",
                "sha1": hashlib.sha1(content, usedforsecurity=False).hexdigest(),
                "sha256": hashlib.sha256(content).hexdigest(),
                "status": "good",
            },
        )


def build_dat(
    driver_name: str,
    description: str,
    entries: dict[str, bytes],
    *,
    parent: tuple[str, str, dict[str, bytes]] | None = None,
) -> bytes:
    root = ElementTree.Element("datafile")
    header = ElementTree.SubElement(root, "header")
    ElementTree.SubElement(header, "name").text = description
    ElementTree.SubElement(header, "description").text = description
    ElementTree.SubElement(header, "version").text = "1"
    attributes = None
    if parent is not None:
        parent_name, parent_description, parent_entries = parent
        _append_machine(root, parent_name, parent_description, parent_entries)
        attributes = {"cloneof": parent_name, "romof": parent_name}
    _append_machine(root, driver_name, description, entries, attributes)
    ElementTree.indent(root, space="  ")
    return b'<?xml version="1.0" encoding="UTF-8"?>\n' + ElementTree.tostring(
        root,
        encoding="utf-8",
    ) + b"\n"
