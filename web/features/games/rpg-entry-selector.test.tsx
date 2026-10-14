import { useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { RuntimeConfigEditor } from "./runtime-config-editor";

const get = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client", async (original) => ({ ...await original<typeof ApiClient>(), api: { GET: get } }));
let entries: string[];
const response = (entries: string[]) => ({ data: { entries }, response: new Response() });
beforeEach(() => {
  entries = ["index.html", "www/index.html"];
  get.mockReset().mockImplementation(async (path: string) => path === "/api/v1/runtime/catalog"
    ? { data: { cores: [], platforms: [], providers: [], bindings: [] }, response: new Response() } : response(entries));
});
afterEach(cleanup);
const files: Schema<"GameFile">[] = ["index.html", "www/index.html", "docs/index.html", "js/rpg_core.js", "data/System.json", "img/title.png"]
  .map(logicalKey => ({ id: logicalKey, logicalKey, role: "content", sha256: "hash", sizeBytes: 1 }));
function Editor({ initial = "index.html", revision = "original" }: { initial?: string; revision?: string }) {
  const [value, setValue] = useState<Schema<"RuntimeConfig">>({ content: { kind: "RPG_MAKER_PROJECT", engine: "RPGMV", entryFile: initial } });
  return <form><RuntimeConfigEditor gameId="game" value={value} coreId="rpgmaker" coreIds={["rpgmaker"]}
    files={files.map(file => ({ ...file, sha256: revision }))} version={1} onUploaded={vi.fn()} onCoreChange={vi.fn()} onChange={setValue} /></form>;
}

it("shows only runtime candidates and preserves the selected full path", async () => {
  render(<Editor />);
  await waitFor(() => expect(get).toHaveBeenCalledWith("/api/v1/admin/games/{gameId}/runtime-options/rpgmaker", expect.anything()));
  const select = screen.getByRole("combobox", { name: "入口文件" });
  expect(screen.queryByRole("textbox", { name: "入口文件" })).not.toBeInTheDocument();
  expect(within(select).getAllByRole("option").map(option => option.getAttribute("value"))).toEqual(["", "index.html", "www/index.html"]);
  fireEvent.change(select, { target: { value: "www/index.html" } });
  expect(select).toHaveValue("www/index.html");
});

it("displays a unique selected entry without an unnecessary selector", async () => {
  entries = ["index.html"];
  render(<Editor />);
  expect(await screen.findByRole("textbox", { name: "入口文件" })).toHaveValue("index.html");
  expect(screen.getByRole("textbox", { name: "入口文件" })).toHaveAttribute("readonly");
  expect(screen.queryByRole("combobox", { name: "入口文件" })).not.toBeInTheDocument();
});

it("automatically selects the only candidate when no entry was selected", async () => {
  entries = ["www/index.html"];
  render(<Editor initial="" />);
  expect(await screen.findByRole("textbox", { name: "入口文件" })).toHaveValue("www/index.html");
});

it("keeps an invalid saved entry visible until explicitly repaired", async () => {
  entries = ["www/index.html"];
  render(<Editor initial="data/System.json" />);
  expect(await screen.findByRole("alert")).toHaveTextContent("data/System.json");
  const select = screen.getByRole("combobox", { name: "入口文件" });
  expect(select).toBeInvalid();
  expect(within(select).getByRole("option", { name: /data\/System.json/ })).toBeDisabled();
  fireEvent.change(select, { target: { value: "www/index.html" } });
  expect(await screen.findByRole("textbox", { name: "入口文件" })).toHaveValue("www/index.html");
});

it("distinguishes unavailable and empty candidates, with retry", async () => {
  get.mockImplementation(async (path: string) => {
    if (path === "/api/v1/runtime/catalog") { return { data: { bindings: [], providers: [], cores: [] }, response: new Response() }; }
    throw new Error("offline");
  });
  render(<Editor />);
  expect(await screen.findByRole("alert")).toHaveTextContent("无法读取入口");
  expect(screen.queryByText(/未找到可用入口/)).not.toBeInTheDocument();
  get.mockResolvedValue(response([]));
  fireEvent.click(screen.getByRole("button", { name: "重试读取入口" }));
  expect(await screen.findByText(/未找到可用入口/)).toBeVisible();
});

it("refreshes on file and engine changes and ignores a stale response", async () => {
  let finish!: (value: ReturnType<typeof response>) => void;
  get.mockImplementation(async (path: string) => path === "/api/v1/runtime/catalog"
    ? { data: { bindings: [], providers: [], cores: [] }, response: new Response() }
    : new Promise<ReturnType<typeof response>>(resolve => { finish = resolve; }));
  const view = render(<Editor />);
  await waitFor(() => expect(finish).toBeDefined());
  get.mockResolvedValue(response(["www/index.html"]));
  view.rerender(<Editor revision="replacement" />);
  expect(await screen.findByRole("alert")).toHaveTextContent("index.html");
  await act(async () => { finish(response(["docs/index.html"])); });
  expect(screen.queryByRole("option", { name: "docs/index.html" })).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole("combobox", { name: "RPG Maker 版本" }), { target: { value: "RPGMZ" } });
  await waitFor(() => expect(get).toHaveBeenLastCalledWith("/api/v1/admin/games/{gameId}/runtime-options/rpgmaker",
    expect.objectContaining({ params: { path: { gameId: "game" }, query: { engine: "RPGMZ" } } })));
});
