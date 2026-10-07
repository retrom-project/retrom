"""Exercise the immutable host-tool boundary used by an explicit PFB build."""
import json
from pathlib import Path
import tempfile
import unittest

from pfb.errors import PFBError
from pfb.runtime_tool import publish_runtime_tool


class RuntimeToolTests(unittest.TestCase):
    def tool(self, root: Path) -> Path:
        tool = root / "tool"
        tool.mkdir()
        (tool / "package.json").write_text(json.dumps({"files": ["dist", "scripts"]}))
        for name, content in {
            "dist/runtime/index.js": "export const fact = 1;",
            "scripts/runtime-cli.mjs": "import '../dist/runtime/index.js';",
            "node_modules/dependency/index.js": "export const dependency = 1;",
        }.items():
            target = tool / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content)
        return tool

    def test_self_contained_snapshot_keeps_dependency_bytes_after_source_changes(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            tool = self.tool(root)
            workspace = root / "workspace"
            workspace.mkdir()
            first = publish_runtime_tool(tool, workspace, self_contained=True)
            current = workspace / "runtime-tool"
            sealed = current.resolve()
            dependency = "node_modules/dependency/index.js"
            original = (current / dependency).read_bytes()
            (tool / dependency).write_text("export const dependency = 2;")
            second = publish_runtime_tool(tool, workspace, self_contained=True)
            self.assertNotEqual(first, second)
            self.assertEqual((sealed / dependency).read_bytes(), original)
            self.assertEqual((current / dependency).read_bytes(), (tool / dependency).read_bytes())
            self.assertFalse((current / "node_modules").is_symlink())

    def test_dependency_link_outside_the_tool_is_rejected_before_publication(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            tool = self.tool(root)
            (root / "outside.js").write_text("outside")
            (tool / "node_modules/dependency/link.js").symlink_to(root / "outside.js")
            workspace = root / "workspace"
            with self.assertRaisesRegex(PFBError, "runtime-package-symlink"):
                publish_runtime_tool(tool, workspace, self_contained=True)
            self.assertFalse((workspace / "runtime-tool").exists())


if __name__ == "__main__":
    unittest.main()
