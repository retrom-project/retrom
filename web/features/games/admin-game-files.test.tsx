import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { AdminGameFiles } from "./admin-game-files";
import type { GameFile } from "./admin-game-manager";

afterEach(cleanup);
const file: GameFile = {
  role: "CONTENT", logicalName: "game.zip", sortOrder: 0, sizeBytes: 1024,
  sha256: "a".repeat(64), md5: "b".repeat(32), sha1: "c".repeat(40), crc32: "12345678", mediaType: "application/zip",
};

describe("AdminGameFiles", () => {
  it("shows complete file identities, disc order and parent archives without write actions", () => {
    render(<AdminGameFiles files={[
      { ...file, logicalName: "disc-2.chd", role: "DISC", sortOrder: 2 },
      { ...file, logicalName: "disc-1.chd", role: "DISC", sortOrder: 1 },
      { ...file, logicalName: "parent.zip", role: "COMPANION", sortOrder: 3 },
      file,
    ]} />);
    const rows = screen.getAllByRole("listitem");
    expect(rows.map((row) => row.querySelector("strong")?.textContent)).toEqual(["game.zip", "disc-1.chd", "disc-2.chd", "parent.zip"]);
    expect(rows[1]).toHaveTextContent("光盘 1");
    expect(rows[2]).toHaveTextContent("光盘 2");
    expect(rows[3]).toHaveTextContent("Parent ROM");
    for (const hash of [file.sha256, file.md5, file.sha1, file.crc32]) {
      expect(within(rows[0]).getByText(hash)).toBeVisible();
    }
    expect(rows[0]).toHaveTextContent("1,024 字节");
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("keeps the primary file first when imported roles reuse sort order zero", () => {
    const view = render(<AdminGameFiles files={[
      { ...file, logicalName: "parent.zip", role: "COMPANION", sortOrder: 0 },
      { ...file, logicalName: "main.zip", role: "CONTENT", sortOrder: 1 },
    ]} />);
    const names = () => screen.getAllByRole("listitem").map((row) => row.querySelector("strong")?.textContent);
    expect(names()).toEqual(["main.zip", "parent.zip"]);
    view.rerender(<AdminGameFiles files={[
      { ...file, logicalName: "game (Disc 1).chd", role: "DISC", sortOrder: 0 },
      { ...file, logicalName: "game.m3u", role: "PLAYLIST_SOURCE", sortOrder: 0 },
      { ...file, logicalName: "game (Disc 2).chd", role: "DISC", sortOrder: 1 },
    ]} />);
    expect(names()).toEqual(["game.m3u", "game (Disc 1).chd", "game (Disc 2).chd"]);
  });

  it("makes every project file reachable without mounting the whole file list", async () => {
    const user = userEvent.setup();
    render(<AdminGameFiles files={Array.from({ length: 21 }, (_, index) => ({ ...file, role: "PROJECT_FILE", logicalName: `data/file-${index}.ks`, sortOrder: index }))} />);
    expect(screen.getAllByRole("listitem")).toHaveLength(20);
    expect(screen.queryByText("data/file-20.ks")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.getByText("data/file-20.ks")).toBeVisible();
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "上一页" }));
    expect(screen.getByText("data/file-0.ks")).toBeVisible();
  });

  it("shows an empty state and omits metadata that has no value", () => {
    const view = render(<AdminGameFiles files={[]} />);
    expect(screen.getByText("暂无游戏文件")).toBeVisible();
    view.rerender(<AdminGameFiles files={[{ ...file, md5: "", sha1: "", crc32: "", mediaType: "" }]} />);
    expect(screen.getByText(file.sha256)).toBeVisible();
    expect(screen.queryByText("MD5")).not.toBeInTheDocument();
    expect(screen.queryByText("CRC32")).not.toBeInTheDocument();
  });
});
