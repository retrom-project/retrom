import {cleanup, fireEvent, render, screen} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {afterEach, expect, it, vi} from "vitest";
import {BIOSFileButton} from "./bios-file-button";

afterEach(cleanup);
const props = {installed:true, attention:false, busy:false, disabled:false, onClick:vi.fn()};

it("only displays replacement guidance on hover or keyboard focus", async () => {
  const user = userEvent.setup();
  render(<BIOSFileButton {...props} />);
  const button = screen.getByRole("button", {name:"替换文件"});
  expect(button).toHaveAccessibleDescription("新 BIOS 将在下次启动游戏时生效，当前运行与已有存档保留");
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  await user.hover(button);
  expect(screen.getByRole("tooltip")).toBeVisible();
  await user.unhover(button);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  await user.tab();
  expect(button).toHaveFocus();
  expect(screen.getByRole("tooltip")).toBeVisible();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
});

it("dismisses floating guidance when scrolling and leaves install actions unchanged", async () => {
  const user = userEvent.setup();
  const onClick = vi.fn();
  const {rerender} = render(<BIOSFileButton {...props} onClick={onClick} />);
  await user.hover(screen.getByRole("button"));
  fireEvent.scroll(window);
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  rerender(<BIOSFileButton {...props} installed={false} onClick={onClick} />);
  await user.hover(screen.getByRole("button", {name:"选择 BIOS 文件"}));
  expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button"));
  expect(onClick).toHaveBeenCalledOnce();
});
