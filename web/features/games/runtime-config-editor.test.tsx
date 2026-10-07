import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { Schema } from "@/lib/api/types";
import { RuntimeConfigEditor } from "./runtime-config-editor";

vi.mock("@/lib/use-resource", () => ({
  useResource: () => ({ data: null, error: "" }),
}));

it("removes an optional DOS program while retaining its archive and per-core options", () => {
  const original: Schema<"RuntimeConfig"> = {
    content: {
      kind: "DOS_BUNDLE",
      entryFile: "game.zip",
      entryPath: "GAME.EXE",
    },
    cores: { dosbox_pure: { options: { cpuType: "auto" } } },
  };
  let next = original;
  const onChange = (value: Schema<"RuntimeConfig">) => {
    next = value;
  };
  const view = render(
    <RuntimeConfigEditor
      value={original}
      gameId="game"
      coreIds={["dosbox_pure"]}
      files={[]}
      onChange={onChange}
    />,
  );
  fireEvent.change(screen.getByLabelText("启动程序"), {
    target: { value: "" },
  });
  expect(next.content).not.toHaveProperty("entryPath");
  expect(next.content.entryFile).toBe("game.zip");
  expect(next.cores).toEqual(original.cores);
  expect(original.content.entryPath).toBe("GAME.EXE");
  view.rerender(
    <RuntimeConfigEditor
      value={next}
      gameId="game"
      coreIds={["dosbox_pure"]}
      files={[]}
      onChange={onChange}
    />,
  );
  expect(screen.getByLabelText("启动程序")).toHaveValue("");
  expect(screen.getByLabelText("启动程序")).toHaveAttribute(
    "placeholder",
    "留空使用核心启动菜单，或填写包内程序路径",
  );
});
