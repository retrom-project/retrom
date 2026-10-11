import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { SaveRestoreWarning } from "./save-restore-warning";
import { saveFixture } from "./test-fixtures";

afterEach(cleanup);
it("covers the main preview with the reason and the thumbnail with an accessible prohibition icon", () => {
  const save = { ...saveFixture("blocked"), restorable: false, restoreReason: "content_changed" as const };
  render(<><SaveRestoreWarning save={save} /><SaveRestoreWarning save={save} thumbnail /></>);
  expect(screen.getByRole("status")).toHaveTextContent("存档无法加载游戏内容已变化，无法恢复");
  const icon = screen.getByRole("img", { name: "存档不可加载：游戏内容已变化，无法恢复" });
  expect(icon.querySelector("svg")).toBeInTheDocument();
  expect(icon).toHaveTextContent("");
});
it("keeps a restorable save's artwork unobscured", () => {
  const { container } = render(<><SaveRestoreWarning save={saveFixture("ready")} /><SaveRestoreWarning save={saveFixture("ready")} thumbnail /></>);
  expect(container).toBeEmptyDOMElement();
});
