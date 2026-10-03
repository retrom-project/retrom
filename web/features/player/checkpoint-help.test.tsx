import {cleanup, screen} from "@testing-library/react";
import {render} from "@/components/toast-test-utils";
import {afterEach, expect, it} from "vitest";
import {NativeSaveToast} from "./checkpoint-help";

afterEach(cleanup);

it("keeps native save failures and successes in the common toast without a player banner", () => {
  const props = {visible: true, semantics: "GAME_SAVE" as const, toast: "", text: "正在保存", tone: "busy"};
  const view = render(<NativeSaveToast {...props} />);
  expect(screen.queryByRole("alert")).toBeNull();
  view.rerender(<NativeSaveToast {...props} tone="warning" text="保存失败，草稿已保留" />);
  expect(screen.getByRole("alert")).toHaveClass("app-toast");
  expect(screen.getByRole("alert")).toHaveTextContent("草稿已保留");
  expect(view.container.querySelector(".player-toast")).toBeNull();
  view.rerender(<NativeSaveToast {...props} tone="synced" toast="保存成功" />);
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByRole("status")).toHaveTextContent("保存成功");
});
