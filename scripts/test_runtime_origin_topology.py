import pathlib
import unittest


REPOSITORY_ROOT = pathlib.Path(__file__).resolve().parents[1]


class RuntimeOriginTopologyTests(unittest.TestCase):
    def test_local_runtime_origin_bypasses_next_and_targets_go(self) -> None:
        script = (REPOSITORY_ROOT / "scripts/acceptance/browser.py").read_text(encoding="utf-8")
        self.assertIn('web_origin = f"http://127.0.0.1:{web_port}"', script)
        self.assertIn('f"http://{{runId}}.rpg.localhost:{backend_port}"', script)
        self.assertNotIn('f"http://{{runId}}.rpg.localhost:{web_port}"', script)
        self.assertIn('"NEXT_BACKEND_ORIGIN": backend_origin', script)


if __name__ == "__main__":
    unittest.main()
