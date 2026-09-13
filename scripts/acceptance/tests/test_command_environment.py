import importlib.util
import os
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location("acceptance_runner_environment", ROOT / "scripts/acceptance/run.py")
assert SPEC and SPEC.loader
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)


class CommandEnvironmentTests(unittest.TestCase):
    def test_commands_use_the_selected_node_toolchain(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "selected-node" / "bin"
            binary.mkdir(parents=True)
            node = binary / "node"
            node.write_text("#!/bin/sh\nprintf 'selected-node-runtime\\n'\n", encoding="utf-8")
            node.chmod(0o700)
            log = root / "command.log"
            environment = dict(os.environ)
            code, timed_out = runner.run_command(
                "node --version", 10, log, {"NODE_HOME": str(binary.parent)},
            )
            self.assertEqual(0, code)
            self.assertFalse(timed_out)
            self.assertEqual("selected-node-runtime\n", log.read_text(encoding="utf-8"))
            self.assertEqual(environment, dict(os.environ))

    def test_commands_without_node_override_preserve_explicit_path(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "command-bin"
            binary.mkdir()
            command = binary / "retrom-environment-probe"
            command.write_text("#!/bin/sh\nprintf 'selected-command\\n'\n", encoding="utf-8")
            command.chmod(0o700)
            log = root / "command.log"
            code, timed_out = runner.run_command(
                "retrom-environment-probe", 10, log,
                {"NODE_HOME": "", "PATH": str(binary) + os.pathsep + os.environ["PATH"]},
            )
            self.assertEqual(0, code)
            self.assertFalse(timed_out)
            self.assertEqual("selected-command\n", log.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
