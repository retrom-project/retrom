"""Image input preparation uses one repository pin; transport options never choose its identity."""
from pathlib import Path
import unittest


class ImageInputTests(unittest.TestCase):
    def test_cli_uses_one_repository_pin_and_transport_only_overrides(self):
        source = Path(__file__).with_name("prepare_image_inputs.py").read_text()
        self.assertIn('ROOT / "data/runtime-inputs.json"', source)
        self.assertNotIn('add_argument("--manifest"', source)
        self.assertIn('add_argument("--archive-root"', source)
        self.assertIn('add_argument("--base-url"', source)
        self.assertNotIn("RETROM_RUNTIME_TOOL_INPUT", source)
        self.assertNotIn("--node", source)


if __name__ == "__main__":
    unittest.main()
