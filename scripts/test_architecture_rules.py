"""Exercise the real depguard configuration against compileable import fixtures."""

import json
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


class ArchitectureRulesTests(unittest.TestCase):
    def test_actual_package_paths_reject_reverse_dependencies(self):
        forbidden = {
            "internal/httpapi": "retrom/internal/persistence/port",
            "internal/httpapi/nested": "database/sql",
            "internal/service/library": "retrom/internal/httpapi/port",
            "internal/service/saves": "retrom/internal/service/scans/port",
            "internal/service/runs": "retrom/internal/service/scans/port",
            "internal/service/scans": "database/sql",
            "internal/service/bios": "os/exec",
            "internal/persistence": "retrom/internal/service/library/port",
            "internal/persistence/nested": "retrom/internal/service/scans/port",
            "internal/storage": "retrom/internal/service/files",
            "internal/temporary": "retrom/internal/httpapi/port",
            "internal/runtimeclient": "retrom/internal/service/scans/port",
            "internal/model": "retrom/internal/persistence/port",
            "internal/format/pegasus": "retrom/internal/service/scans/port",
            "internal/format/emulationstation": "os/exec",
            "internal/model/nested": "retrom/internal/httpapi/port",
        }
        allowed = {
            "cmd/retrom": "retrom/internal/service/library/port",
            "internal/httpapi/legal": "retrom/internal/service/directory/port",
            "internal/service/directory": "retrom/internal/persistence/directory",
            "internal/persistence/legal": "retrom/internal/model/legal",
            "internal/runtimeclient/legal": "os/exec",
            "internal/storage/legal": "retrom/internal/model/legal",
        }
        with tempfile.TemporaryDirectory(prefix="retrom-architecture-") as directory:
            root = Path(directory)
            version = (ROOT / "go.mod").read_text().split("\ngo ")[1].splitlines()[0]
            (root / "go.mod").write_text(f"module retrom\n\ngo {version}\n")
            for path, dependency in {**forbidden, **allowed}.items():
                target = root / path
                target.mkdir(parents=True, exist_ok=True)
                (target / "boundary.go").write_text(f'package fixture\nimport _ "{dependency}"\n')
            for dependency in {value for value in {*forbidden.values(), *allowed.values()} if value.startswith("retrom/")}:
                target = root / dependency.removeprefix("retrom/")
                target.mkdir(parents=True, exist_ok=True)
                (target / "stub.go").write_text("package fixture\n")
            result = subprocess.run([
                str(ROOT / "bin/golangci-lint"), "run", "--config", str(ROOT / ".golangci.yml"),
                "--enable-only", "depguard", "--output.text.path", "/dev/null",
                "--output.json.path", "stdout", "--show-stats=false", "./...",
            ], cwd=root, text=True, capture_output=True, timeout=120, check=False)
            self.assertEqual(result.returncode, 1, result.stderr + result.stdout)
            issues = json.loads(result.stdout)["Issues"]
            actual = {str(Path(issue["Pos"]["Filename"]).parent) for issue in issues}
            self.assertTrue(all(issue["FromLinter"] == "depguard" for issue in issues), issues)
            self.assertEqual(actual, set(forbidden), result.stdout)


if __name__ == "__main__":
    unittest.main()
