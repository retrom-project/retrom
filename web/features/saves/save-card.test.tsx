import type { ReactNode } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import type { Save } from "@/lib/api/types";
import { SaveCard } from "./save-card";

vi.mock("@/components/toast-provider", () => ({
  useToast: () => ({ notify: vi.fn() }),
}));
vi.mock("@/features/player/launch-button", () => ({
  LaunchButton: ({ children }: { children: ReactNode }) => <button>{children}</button>,
}));

afterEach(cleanup);

function save(id: string): Save {
  return {
    id, name: `进度 ${id}`, kind: "checkpoint", slot: null, version: 1,
    sizeBytes: 1024, createdAtMs: 0, updatedAtMs: 0, screenshotUrl: null,
    restorable: false, restoreReason: "core_changed",
    game: {
      id: "game", platformInstanceId: "directory", platformId: "nes", directoryName: "NES",
      title: "测试游戏", description: "", developer: "", publisher: "", genre: "", players: null,
      releaseYear: null, status: "published", source: "server_import", version: 1,
      contentHash: "a".repeat(64), tags: [], media: [], favorite: false, lastPlayedAtMs: null, createdAtMs: 0, updatedAtMs: 0,
    },
    extinfo: {
      coreId: "fceumm", providerId: "emulatorjs", targetId: "fceumm",
      coreFingerprint: "b".repeat(64), romHash: "a".repeat(64), checkpointFormat: "state-storage-v1",
      content: { kind: "SINGLE_FILE", entryFile: "game.nes" }, runtimeOptions: {},
    },
  };
}

it("closes for an outside pointer without swallowing its click or stealing input focus", async () => {
  const user = userEvent.setup();
  const outside = vi.fn();
  render(<><SaveCard save={save("1")} /><label>搜索<input onClick={outside} /></label></>);
  await user.click(screen.getByRole("button", { name: "进度 1的更多操作" }));
  expect(screen.getByRole("menu")).toBeInTheDocument();
  const search = screen.getByRole("textbox", { name: "搜索" });
  await user.click(search);
  expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  expect(search).toHaveFocus();
  expect(outside).toHaveBeenCalledOnce();
  await user.type(search, "进度");
  expect(search).toHaveValue("进度");
});

it("keeps only the newly activated card menu open for pointer and keyboard clicks", async () => {
  const user = userEvent.setup();
  render(<><SaveCard save={save("1")} /><SaveCard save={save("2")} /></>);
  const first = screen.getByRole("button", { name: "进度 1的更多操作" });
  const second = screen.getByRole("button", { name: "进度 2的更多操作" });
  await user.click(first);
  await user.click(second);
  expect(first).toHaveAttribute("aria-expanded", "false");
  expect(second).toHaveAttribute("aria-expanded", "true");
  expect(screen.getAllByRole("menu")).toHaveLength(1);
  await user.click(second);
  expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  first.focus();
  await user.keyboard("{Enter}");
  second.focus();
  await user.keyboard("{Enter}");
  expect(first).toHaveAttribute("aria-expanded", "false");
  expect(second).toHaveAttribute("aria-expanded", "true");
  expect(screen.getAllByRole("menu")).toHaveLength(1);
});

it("closes with Escape from inside the menu and returns focus to its trigger", async () => {
  const user = userEvent.setup();
  render(<SaveCard save={save("1")} />);
  const trigger = screen.getByRole("button", { name: "进度 1的更多操作" });
  await user.click(trigger);
  screen.getByRole("menuitem", { name: "重命名" }).focus();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  expect(trigger).toHaveAttribute("aria-expanded", "false");
  expect(trigger).toHaveFocus();
});

it.each([
  ["重命名", "存档名称"],
  ["删除存档", "删除存档"],
])("preserves the %s action through pointer capture and modal cancellation", async (action, title) => {
  const user = userEvent.setup();
  render(<SaveCard save={save("1")} />);
  await user.click(screen.getByRole("button", { name: "进度 1的更多操作" }));
  await user.click(screen.getByRole("menuitem", { name: action }));
  expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  expect(screen.getByRole("alertdialog", { name: title })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "取消" })).toHaveFocus();
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
});

it("releases Escape ownership when the open card unmounts", async () => {
  const user = userEvent.setup();
  const key = vi.fn();
  const view = render(<SaveCard save={save("1")} />);
  await user.click(screen.getByRole("button", { name: "进度 1的更多操作" }));
  view.rerender(<button onKeyDown={key}>外部操作</button>);
  const outside = screen.getByRole("button", { name: "外部操作" });
  outside.focus();
  await user.keyboard("{Escape}");
  expect(key).toHaveBeenCalledOnce();
  expect(key.mock.calls[0][0].defaultPrevented).toBe(false);
  expect(outside).toHaveFocus();
});
