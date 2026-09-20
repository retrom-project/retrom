import hashlib
from pathlib import Path
import struct
import tempfile
import unittest
import zlib
from scripts.acceptance.psp_run_disc import create_run_disc


class PSPRunDiscTests(unittest.TestCase):
    def test_iso_and_cso_preserve_every_original_sector_and_declare_provenance(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            original = bytearray(18 * 2048)
            original[:2048] = b"".join(hashlib.sha256(str(i).encode()).digest() for i in range(64))
            original[32768:32775] = b"\x01CD001\x01"
            struct.pack_into("<I", original, 32768 + 80, 18)
            struct.pack_into(">I", original, 32768 + 84, 18)
            source = root / "owned.iso"
            source.write_bytes(original)
            for format_name in ("iso", "cso"):
                output = root / ("run." + format_name)
                receipt = create_run_disc(source, output, format_name, "11111111-1111-4111-8111-111111111111")
                encoded = output.read_bytes()
                decoded = encoded if format_name == "iso" else self.decode_cso(encoded)
                self.assertEqual(decoded[:len(original)], original)
                self.assertEqual(source.read_bytes(), original)
                self.assertEqual(len(decoded), len(original) + 2048)
                self.assertTrue(decoded[len(original):].startswith(b"RETROM_ACCEPTANCE_RUN:"))
                self.assertEqual(receipt["outputSha256"], hashlib.sha256(encoded).hexdigest())
                with self.assertRaises(FileExistsError):
                    create_run_disc(source, output, format_name, receipt["runId"])

    def decode_cso(self, data):
        magic, header, size, block, version, alignment = struct.unpack_from("<4sIQIBB2x", data)
        self.assertEqual((magic, header, block, version, alignment), (b"CISO", 24, 2048, 1, 0))
        indexes = struct.unpack_from(f"<{size // block + 1}I", data, header)
        result, modes = bytearray(), set()
        for start, end in zip(indexes, indexes[1:]):
            plain = bool(start & 0x80000000)
            modes.add(plain)
            chunk = data[start & 0x7FFFFFFF:end & 0x7FFFFFFF]
            decoded = chunk if plain else zlib.decompress(chunk, -15)
            self.assertEqual(len(decoded), block)
            result.extend(decoded)
        self.assertEqual(modes, {False, True})
        return result

    def test_trimmed_iso_keeps_marker_outside_declared_volume(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            original = bytearray(18 * 2048)
            original[32768:32775] = b"\x01CD001\x01"
            struct.pack_into("<I", original, 32768 + 80, 19)
            struct.pack_into(">I", original, 32768 + 84, 19)
            source = root / "trimmed.iso"
            source.write_bytes(original)
            output = root / "derived.iso"
            receipt = create_run_disc(source, output, "iso", "11111111-1111-4111-8111-111111111111")
            actual = output.read_bytes()
            self.assertEqual(actual[:len(original)], original)
            self.assertEqual(actual[len(original):19 * 2048], bytes(2048))
            self.assertTrue(actual[19 * 2048:].startswith(b"RETROM_ACCEPTANCE_RUN:"))
            self.assertEqual(receipt["paddedEmptySectors"], 1)
