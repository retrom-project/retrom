import { useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { api } from "@/lib/api/client";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { DOSEntrySelector } from "./dos-entry-selector";
import { RuntimeConfigEditor } from "./runtime-config-editor";

const get = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client", async (importOriginal) => ({
  ...await importOriginal<typeof ApiClient>(), api: { GET: get },
}));
const getCandidates = api.GET<"/api/v1/admin/games/{gameId}/runtime-options/dos", { params: { path: { gameId: string }; query: { entryFile: string } } }>;
beforeEach(() => vi.resetAllMocks());
afterEach(cleanup);

it("searches complete paths without changing selection until an option is chosen", async () => {
  vi.mocked(getCandidates).mockResolvedValue({ data: { entries: ["START.EXE", "tools/SETUP.COM", "中文目录/play.BaT"] }, response: new Response() });
  function Editor() { const [value, setValue] = useState("START.EXE"); return <DOSEntrySelector gameId="game" entryFile="game.zip" value={value} onSelect={setValue} />; }
  render(<Editor />);
  const input = screen.getByRole("combobox", { name: "启动程序" });
  await waitFor(() => expect(api.GET).toHaveBeenCalledTimes(1));
  fireEvent.focus(input);
  fireEvent.change(input, { target: { value: "setup" } });
  expect(await screen.findByRole("option", { name: "tools/SETUP.COM" })).toBeVisible();
  expect(screen.queryByRole("option", { name: "START.EXE" })).not.toBeInTheDocument();
  fireEvent.keyDown(input, { key: "Escape" });
  expect(input).toHaveValue("START.EXE");
  fireEvent.focus(input); fireEvent.change(input, { target: { value: "play" } }); fireEvent.keyDown(input, { key: "Enter" });
  expect(input).toHaveValue("中文目录/play.BaT");
  fireEvent.focus(input); fireEvent.click(screen.getByRole("option", { name: "使用核心启动菜单" }));
  expect(input).toHaveValue("使用核心启动菜单");
});

it("keeps an unavailable old path visible and invalid until the user explicitly selects a replacement", async () => {
  vi.mocked(getCandidates).mockResolvedValue({ data: { entries: ["NEW.EXE"] }, response: new Response() });
  const select = vi.fn();
  render(<DOSEntrySelector gameId="game" entryFile="new.zip" value="old/folder/START.EXE" onSelect={select} />);
  const input = screen.getByRole("combobox", { name: "启动程序" });
  expect(await screen.findByRole("alert")).toHaveTextContent("old/folder/START.EXE");
  expect(input).toHaveValue("old/folder/START.EXE");expect(input).toBeInvalid();expect(select).not.toHaveBeenCalled();
});

it("preserves the existing choice on read failure and can retry without claiming it is missing", async () => {
  vi.mocked(getCandidates).mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce({ data: { entries: ["OLD.EXE"] }, response: new Response() });
  render(<DOSEntrySelector gameId="game" entryFile="game.zip" value="OLD.EXE" onSelect={vi.fn()} />);
  expect(await screen.findByRole("alert")).toHaveTextContent("无法读取包内启动程序");
  expect(screen.getByRole("combobox")).toHaveValue("OLD.EXE");expect(screen.getByRole("combobox")).toBeValid();
  expect(screen.queryByText(/在当前游戏包中不存在/)).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "重试读取" }));
  await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
});

it("refreshes candidates when files change and ignores the previous archive's late response", async () => {
  let finishOld!: (value: Awaited<ReturnType<typeof getCandidates>>) => void;
  let reads = 0;
  get.mockImplementation(async (path: string) => {
    if (path === "/api/v1/runtime/catalog") { return { data: { cores: [], platforms: [], providers: [], bindings: [] }, response: new Response() }; }
    reads++;
    if (reads === 1) { return new Promise<Awaited<ReturnType<typeof getCandidates>>>((resolve) => { finishOld = resolve; }); }
    return { data: { entries: ["new/NOW.EXE"] }, response: new Response() };
  });
  const config: Schema<"RuntimeConfig"> = { content: { kind: "DOS_BUNDLE", entryFile: "game.zip", entryPath: "old/BEFORE.EXE" } };
  const file: Schema<"GameFile"> = { id: "old", logicalKey: "game.zip", role: "content", sha256: "old-hash", sizeBytes: 100 };
  const view = render(<RuntimeConfigEditor coreId="" onCoreChange={vi.fn()} version={1} onUploaded={vi.fn()} gameId="game" value={config} coreIds={[]} files={[file]} onChange={vi.fn()} />);
  await waitFor(() => expect(reads).toBe(1));
  view.rerender(<RuntimeConfigEditor coreId="" onCoreChange={vi.fn()} version={1} onUploaded={vi.fn()} gameId="game" value={config} coreIds={[]} files={[{ ...file, id: "new", sha256: "new-hash" }]} onChange={vi.fn()} />);
  expect(await screen.findByRole("alert")).toHaveTextContent("old/BEFORE.EXE");
  await act(async () => finishOld({ data: { entries: ["old/BEFORE.EXE"] }, response: new Response() }));
  fireEvent.focus(screen.getByRole("combobox", { name: "启动程序" }));
  expect(screen.getByRole("option", { name: "new/NOW.EXE" })).toBeVisible();
  expect(screen.queryByRole("option", { name: "old/BEFORE.EXE" })).not.toBeInTheDocument();
  expect(reads).toBe(2);
});
