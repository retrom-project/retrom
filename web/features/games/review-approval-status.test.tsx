import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { ReviewApprovalStatus } from "./review-approval-status";

afterEach(cleanup);
it("lists actual missing filenames beneath the game count and preserves links to review and dependency management", () => {
  const game = { id: "game", title: "BBC test", version: 1 };
  const files = ["BASIC.ROM", "DFS-1.2.rom", "os.rom"].map((name) => ({ key: `runtime/bbc/${name}`, name, coreId: "jsbeeb" }));
  render(<ReviewApprovalStatus busy={false} summary={{ total: 1, checked: 1, approved: 0, missingBios: 1, failed: 0, interrupted: false, failures: [], missingBiosDetails: [{ game, requirements: files }] }} />);
  const toggle = screen.getByText("缺少 BIOS 的游戏（1 款）");
  fireEvent.click(toggle);
  const details = toggle.closest("details")!;
  for (const name of ["BASIC.ROM", "DFS-1.2.rom", "os.rom"]) { expect(within(details).getByText(name)).toBeVisible(); }
  expect(within(details).getByRole("link", { name: "BBC test" })).toHaveAttribute("href", "/admin/reviews/game");
  expect(within(details).getByRole("link", { name: "管理运行依赖" })).toHaveAttribute("href", "/admin/bios");
  expect(screen.queryByText("查看失败条目")).not.toBeInTheDocument();
});
