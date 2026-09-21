"""Deterministic parser tests: only synthetic source declarations, no BIOS bytes."""
import hashlib
import io
import json
import tarfile
import tempfile
import unittest
from pathlib import Path

from firmware_catalog import generate, members_for


LOAD = 'ROM_LOAD("boot.rom", 0x0, 0x10, CRC(12345678) SHA1(' + 'a' * 40 + '))'


class FirmwareCatalogTests(unittest.TestCase):
    def test_default_bios_and_common_members(self):
        driver = '''ROM_START(test)
ROM_SYSTEM_BIOS(0, "first", "First")
ROM_SYSTEM_BIOS(1, "second", "Second")
ROM_DEFAULT_BIOS("second")
''' + LOAD.replace('ROM_LOAD', 'ROMX_LOAD').replace('))', '), ROM_BIOS(0))') + '\n' + LOAD.replace('boot', 'second').replace('ROM_LOAD', 'ROMX_LOAD').replace('))', '), ROM_BIOS(1))') + '\n' + LOAD.replace('boot', 'servo') + '\nROM_END'
        members = members_for(driver, 'test')
        self.assertEqual([m['required'] for m in members], [False, True, True])

    def test_fail_closed_on_unsupported_or_unsafe_declarations(self):
        for declaration in [LOAD + '\nROM_COPY("x", 0, 0, 1)', LOAD.replace('0x10', 'UNKNOWN'),
                            LOAD.replace('boot.rom', '../boot.rom'), LOAD.replace('boot.rom', '.'),
                            LOAD.replace('CRC(12345678)', 'NO_DUMP'), LOAD + '\n' + LOAD,
                            '#ifdef X\n' + LOAD + '\n#endif', LOAD + '\nROM_DEFAULT_BIOS("absent")']:
            with self.subTest(declaration=declaration), self.assertRaises(ValueError):
                members_for('ROM_START(test)\n' + declaration + '\nROM_END', 'test')

    def test_pinned_archive_and_metadata_select_upload_units(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / 'source.tar.gz'
            with tarfile.open(archive, 'w:gz') as output:
                for name, contents in {
                    'core.info': 'firmware_count = 1\nfirmware0_path = "core/bios/test.zip"\nfirmware0_opt = "false"',
                    'driver.cpp': 'ROM_START(test)\n' + LOAD + '\nROM_END',
                }.items():
                    value = contents.encode()
                    info = tarfile.TarInfo('source/' + name)
                    info.size = len(value)
                    output.addfile(info, io.BytesIO(value))
            recipe = {'infoPath': 'core.info', 'romSourcePath': 'driver.cpp',
                      'sourceSha256': hashlib.sha256(archive.read_bytes()).hexdigest()}
            catalog = generate(recipe, archive)
            self.assertEqual(catalog['items'][0]['logicalName'], 'test.zip')
            self.assertEqual(catalog['items'][0]['emulatorPath'], '/core/bios/test.zip')
            self.assertEqual(catalog['items'][0]['mode'], 'REQUIRED')
            self.assertEqual(len(catalog['items'][0]['members']), 1)
            recipe['sourceSha256'] = '0' * 64
            with self.assertRaisesRegex(ValueError, 'digest mismatch'):
                generate(recipe, archive)

    def test_generated_catalog_retains_the_exact_recipe(self):
        root = Path(__file__).resolve().parents[1] / 'internal/firmwaremanifest'
        self.assertEqual(json.loads((root / 'catalog.json').read_text())['source'],
                         json.loads((root / 'source.json').read_text()))


if __name__ == '__main__':
    unittest.main()
