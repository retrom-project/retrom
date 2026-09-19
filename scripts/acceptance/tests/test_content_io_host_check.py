"""Host check orchestration uses bounded real subprocesses and confined evidence paths."""
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

from scripts.acceptance import content_io_host_check as check


class ContentIOHostCheckTests(unittest.TestCase):
    def test_rejects_foreign_environment_and_escaping_output(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            evidence = root / ".pfb/workspace/content-io"
            evidence.mkdir(parents=True)
            env = evidence / "environment.json"
            value = {"repositories": {"retrom": {"root": str(root)}}, "paths": {"evidenceRoot": str(evidence)}}
            env.write_text(json.dumps(value))
            with patch.object(check, "ROOT", root):
                self.assertEqual(check.validate_paths(env, evidence / "new"), value)
                with self.assertRaisesRegex(ValueError, "PATH_INVALID"):
                    check.validate_paths(env, root / "foreign")
                alias = evidence / "alias"
                alias.symlink_to(root, target_is_directory=True)
                with self.assertRaisesRegex(ValueError, "PATH_INVALID"):
                    check.validate_paths(env, alias / "new")
                value["repositories"]["retrom"]["root"] = str(root / "other")
                env.write_text(json.dumps(value))
                with self.assertRaisesRegex(ValueError, "ENVIRONMENT_MISMATCH"):
                    check.validate_paths(env, evidence / "new")

    def test_records_failure_and_enforces_the_hard_timeout(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            failed = check.execute([sys.executable, "-c", "print('owned failure'); raise SystemExit(17)"], output, 0)
            self.assertEqual(failed["exitCode"], 17)
            self.assertFalse(failed["timedOut"])
            self.assertEqual((output / failed["stdout"]).read_text(), "owned failure\n")
            hung = check.execute([sys.executable, "-c", "import time; time.sleep(60)"], output, 1, timeout=0.1)
            self.assertTrue(hung["timedOut"])
            self.assertNotEqual(hung["exitCode"], 0)
            self.assertEqual(json.loads((output / "1.command.json").read_text()), hung)

    def test_failed_command_stops_the_suite_without_a_pass_record(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            with patch.object(check, "commands", return_value=[[sys.executable, "-c", "raise SystemExit(9)"], ["must-not-run"]]):
                self.assertEqual(check.inside(output), 1)
            report = json.loads((output / "host-check.json").read_text())
            self.assertEqual(report["status"], "FAIL")
            self.assertEqual(len(report["commands"]), 1)

    def test_quality_build_restores_only_its_generated_next_declaration(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "web").mkdir()
            declaration = root / "web/next-env.d.ts"
            original = 'import "./.next/types/routes.d.ts";\n'
            declaration.write_text(original)

            def build(*_args):
                declaration.write_text(original.replace("./.next/", "./.next-build/"))
                return {"exitCode": 17, "timedOut": False}

            with patch.object(check, "ROOT", root), patch.object(check, "execute", side_effect=build):
                self.assertEqual(check.inside(root, quality=True), 1)
            self.assertEqual(declaration.read_text(), original)


if __name__ == "__main__":
    unittest.main()
