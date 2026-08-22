import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ImportSetup } from "./import-setup";

afterEach(cleanup);

describe("ImportSetup", () => {
  it("directs an administrator to create recommended directories before importing", () => {
    render(<ImportSetup directories={[]} />);

    expect(screen.getByRole("heading", { name: "还没有游戏目录" })).toBeVisible();
    expect(screen.getByText(/一键创建推荐目录/)).toBeVisible();
    expect(screen.getByRole("link", { name: "前往游戏目录" })).toHaveAttribute("href", "/admin/platform-instances");
    expect(screen.queryByRole("button", { name: "选择文件" })).not.toBeInTheDocument();
  });
});
