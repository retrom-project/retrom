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
            "internal/service/gamecontent": "retrom/internal/service/jobs",
            "internal/service/launch": "retrom/internal/service/libraryimport",
            "internal/service/gamevariant": "retrom/internal/libraryimport",
            "internal/service/gamevariant/nested": "retrom/internal/service/libraryimport/port",
            "internal/launch": "retrom/internal/persistence/libraryimport",
            "internal/persistence/launch": "retrom/internal/service/libraryimport/port",
            "internal/persistence/gamevariant/nested": "retrom/internal/persistence/libraryimport",
            "internal/composition/launch": "retrom/internal/composition/libraryimport",
            "internal/composition/gamevariant": "retrom/internal/composition/importworkflow",
            "internal/content/arcade": "retrom/internal/service/libraryimport/port",
            "internal/content/arcade/nested": "retrom/internal/persistence/arcade",
            "internal/service/metadatascrape": "retrom/internal/service/jobs",
            "internal/service/saves": "retrom/internal/service/jobs",
            "internal/service/uploads/nested": "retrom/internal/service/jobs",
            "internal/service/libraryimport": "retrom/internal/application",
            "internal/service/firmware": "retrom/internal/composition/fixture",
            "internal/service/catalog": "retrom/internal/httpapi",
            "internal/filestore": "retrom/internal/service/gameassets",
            "internal/store": "retrom/internal/application",
            "internal/format/arcadedat": "retrom/internal/service/metadatascrape",
            "internal/runtime/fixture": "retrom/internal/composition/fixture",
            "internal/httpapi": "retrom/internal/application",
            "internal/httpapi/assembly": "retrom/internal/composition/fixture",
            "internal/httpapi/database": "retrom/internal/database",
            "internal/httpapi/repository": "retrom/internal/persistence/libraryimport",
            "internal/httpapi/storage": "retrom/internal/store",
        }
        allowed = {
            "internal/persistence/libraryimport": "retrom/internal/service/libraryimport/port",
            "internal/service/gameassets": "retrom/internal/filestore/port",
            "internal/composition/fixture": "retrom/internal/service/libraryimport/port",
            "internal/httpapi/legal": "retrom/internal/service/libraryimport/port",
        }
        with tempfile.TemporaryDirectory(prefix="retrom-architecture-") as directory:
            root = Path(directory)
            version = (ROOT / "go.mod").read_text().split("\ngo ")[1].splitlines()[0]
            (root / "go.mod").write_text(f"module retrom\n\ngo {version}\n")
            for path, dependency in {**forbidden, **allowed}.items():
                target = root / path
                target.mkdir(parents=True, exist_ok=True)
                (target / "boundary.go").write_text(f'package fixture\nimport _ "{dependency}"\n')
            for dependency in {*forbidden.values(), *allowed.values()}:
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
