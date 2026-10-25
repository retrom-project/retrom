import {afterEach, expect, it, vi} from "vitest";
import {cleanup, fireEvent, render, screen} from "@testing-library/react";
import {PlayerStartupChoice} from "./player-startup-choice";

afterEach(cleanup);
it("keeps startup interactive and selects the second entry by keyboard", () => {
  const onChoose = vi.fn(), onCancel = vi.fn();
  render(<PlayerStartupChoice interaction={{id: "entry", title: "选择启动入口", choices: [{id: "2", label: "第二项"}, {id: "3", label: "第三项"}]}} onChoose={onChoose} onCancel={onCancel} />);
  expect(document.activeElement).toBe(screen.getByRole("button", {name: "第二项"}));
  fireEvent.keyDown(document.activeElement!, {key: "ArrowDown"});
  expect(document.activeElement).toBe(screen.getByRole("button", {name: "第三项"}));
  fireEvent.click(document.activeElement!);
  expect(onChoose).toHaveBeenCalledWith("3");
  fireEvent.keyDown(document.activeElement!, {key: "Escape"});
  expect(onCancel).toHaveBeenCalledOnce();
});
