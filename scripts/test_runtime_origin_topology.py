import pathlib
import unittest


REPOSITORY_ROOT = pathlib.Path(__file__).resolve().parents[1]


class RuntimeOriginTopologyTests(unittest.TestCase):
    def test_local_runtime_origin_bypasses_next_and_targets_go(self) -> None:
        script = (REPOSITORY_ROOT / "scripts/acceptance/web-e2e.sh").read_text(encoding="utf-8")
        self.assertIn('web_origin="http://retrom-app.rpg.localhost:${web_port}"', script)
        self.assertNotIn('web_origin="http://app.rpg.localhost:${web_port}"', script)
        self.assertNotIn('web_origin="http://localhost:${web_port}"', script)
        self.assertIn(
            'runtime_origin_template="http://{launchId}.rpg.localhost:${backend_port}"',
            script,
        )
        self.assertNotIn(
            'runtime_origin_template="http://{launchId}.rpg.localhost:${web_port}"',
            script,
        )


if __name__ == "__main__":
    unittest.main()
