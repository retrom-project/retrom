"""Generate archive firmware requirements from pinned libretro source metadata."""
import argparse
import hashlib
import json
import re
import tarfile
from pathlib import Path, PurePosixPath


def source_text(archive, name):
    matches = [m for m in archive.getmembers() if m.name == name or m.name.endswith('/' + name)]
    if len(matches) != 1 or not matches[0].isfile() or matches[0].size > 8 * 1024 * 1024:
        raise ValueError(f'Expected one bounded source file: {name}')
    return archive.extractfile(matches[0]).read().decode('utf-8')


def members_for(driver, machine):
    blocks = re.findall(r'ROM_START\(\s*' + re.escape(machine) + r'\s*\)(.*?)ROM_END', driver, re.S)
    if len(blocks) != 1:
        raise ValueError(f'Unresolved ROM set: {machine}')
    block = re.sub(r'//[^\n]*|/\*.*?\*/', '', blocks[0], flags=re.S)
    if '#' in block:
        raise ValueError('Conditional ROM declarations need an authoritative exported DAT')
    default = re.search(r'ROM_DEFAULT_BIOS\(\s*"([^"]+)"\s*\)', block)
    variants = re.findall(r'ROM_SYSTEM_BIOS\(\s*(\d+)\s*,\s*"([^"]+)"\s*,\s*"([^"]+)"\s*\)', block)
    selected = next((int(i) for i, name, _ in variants if default and name == default.group(1)), 0)
    if default and not any(name == default.group(1) for _, name, _ in variants):
        raise ValueError('Unknown default BIOS')
    if variants and (selected not in {int(i) for i, _, _ in variants} or
                     len({int(i) for i, _, _ in variants}) != len(variants) or
                     len({name for _, name, _ in variants}) != len(variants)):
        raise ValueError('Ambiguous BIOS variants')
    result = []
    allowed = {'ROM_REGION', 'ROM_SYSTEM_BIOS', 'ROM_DEFAULT_BIOS', 'ROM_LOAD', 'ROMX_LOAD', 'ROM_LOAD16_WORD_SWAP', 'ROM_BIOS'}
    if set(re.findall(r'\b(ROM\w+)\s*\(', block)) - allowed:
        raise ValueError('Unsupported ROM declaration; do not emit an incomplete catalog')
    pattern = r'ROM(?:X)?_LOAD(?:16_WORD_SWAP)?\s*\(\s*"([^"]+)"\s*,\s*0x[0-9a-fA-F]+\s*,\s*(0x[0-9a-fA-F]+)\s*,([^\n]+)'
    loads = re.findall(pattern, block)
    if len(loads) != len(re.findall(r'\bROM(?:X)?_LOAD(?:16_WORD_SWAP)?\s*\(', block)):
        raise ValueError('Unparsed ROM load declaration')
    for name, size, flags in loads:
        path = PurePosixPath(name)
        if path.is_absolute() or '..' in path.parts or str(path) != name or name in ("", ".") or any(ord(c) < 32 for c in name) or '\\' in name:
            raise ValueError('Unsafe ROM member path')
        crc, sha = re.search(r'CRC\(([0-9a-fA-F]{8})\)', flags), re.search(r'SHA1\(([0-9a-fA-F]{40})\)', flags)
        bios = re.search(r'ROM_BIOS\((\d+)\)', flags)
        if not crc or not sha or 'NO_DUMP' in flags:
            raise ValueError('ROM member has no verifiable content identity')
        if bios and int(bios.group(1)) not in {int(i) for i, _, _ in variants}:
            raise ValueError('Unknown BIOS selector')
        result.append({'name': name, 'sizeBytes': int(size, 16), 'crc32': crc.group(1).lower(),
                       'sha1': sha.group(1).lower(), 'required': not bios or int(bios.group(1)) == selected})
    if not result or len({m['name'].lower() for m in result}) != len(result) or not any(m['required'] for m in result):
        raise ValueError('Empty or ambiguous ROM set')
    return result


def generate(recipe, archive_path):
    digest = hashlib.sha256(Path(archive_path).read_bytes()).hexdigest()
    if digest != recipe['sourceSha256']:
        raise ValueError('Source archive digest mismatch')
    with tarfile.open(archive_path) as archive:
        info = source_text(archive, recipe['infoPath'])
        driver = source_text(archive, recipe['romSourcePath'])
    fields = re.findall(r'^(firmware\w+)\s*=\s*"([^"]*)"', info, re.M)
    metadata = dict(fields)
    if len(fields) != len(metadata):
        raise ValueError('Duplicate firmware metadata')
    count = re.search(r'^firmware_count\s*=\s*(\d+)', info, re.M)
    if not count or not 0 < int(count.group(1)) <= 64:
        raise ValueError('Missing firmware metadata')
    items = []
    for index in range(int(count.group(1))):
        path = metadata[f'firmware{index}_path']
        optional = metadata[f'firmware{index}_opt']
        name = PurePosixPath(path)
        if optional not in ('true', 'false') or name.is_absolute() or '..' in name.parts or str(name) != path or '\\' in path or any(ord(c) < 32 for c in path) or name.suffix != '.zip':
            raise ValueError('Unsupported firmware metadata')
        items.append({'logicalName': name.name, 'emulatorPath': '/' + path,
                      'mode': 'OPTIONAL' if optional == 'true' else 'REQUIRED',
                      'members': members_for(driver, name.stem)})
    if len({item['logicalName'].lower() for item in items}) != len(items):
        raise ValueError('Ambiguous firmware upload name')
    return {'schemaVersion': 1, 'parserVersion': 'LIBRETRO_ROM_SETS_V1', 'source': recipe, 'items': items}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--recipe', required=True)
    parser.add_argument('--source-archive', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    value = generate(json.loads(Path(args.recipe).read_text()), args.source_archive)
    output = json.dumps(value, ensure_ascii=False, indent=2) + '\n'
    if args.check:
        if Path(args.output).read_text() != output:
            raise SystemExit('Firmware catalog differs from pinned source')
    else:
        Path(args.output).write_text(output)


if __name__ == '__main__':
    main()
