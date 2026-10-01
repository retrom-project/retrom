import {cleanup, render, screen} from "@testing-library/react";
import {afterEach, expect, it} from "vitest";
import {PlayerLoading} from "./player-loading";

afterEach(cleanup);
it("renders completed tasks, real percentages and unknown work in the same three-row list", () => {
  render(<PlayerLoading immersive={false} message="正在启动游戏…" progress={null} returnTo="/library" state="loading" tasks={[
    {id: "1", kind: "BIOS", state: "COMPLETED", progress: null},
    {id: "2", kind: "GAME_CONTENT", state: "RUNNING", progress: {loadedBytes: 72, totalBytes: 100}},
    {id: "3", kind: "CORE_INITIALIZATION", state: "RUNNING", progress: null},
  ]} />);
  expect(screen.getByText("BIOS 已就绪")).toBeVisible();
  expect(screen.getByRole("progressbar", {name: "游戏内容准备中"})).toHaveAttribute("aria-valuenow", "72");
  expect(screen.getByText("核心初始化中")).toBeVisible();
  expect(screen.getAllByRole("listitem")).toHaveLength(3);
});
