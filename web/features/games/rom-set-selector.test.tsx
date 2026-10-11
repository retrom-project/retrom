import { useState } from "react";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { ROMSetSelector } from "./rom-set-selector";
import { RuntimeConfigEditor } from "./runtime-config-editor";

const get = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client", async original => ({ ...await original<typeof ApiClient>(), api: { GET: get } }));
const cores = ["fbneo", "mame2003", "mame2003_plus", "mame_arcade"];
const digest = "a".repeat(64);
const entry = (name: string) => ({ name, description: `游戏 ${name}`, year: "1990", manufacturer: "厂商" });
function response(coreId: string, romSet = "") {
  return { data: { coreId, datSHA256: digest, automaticSelection: "alpha", selected: entry(romSet || "alpha"),
    entries: [entry("alpha"), entry("beta")], total: 2, offset: 0, limit: 30 }, response: new Response() };
}
beforeEach(() => {
  vi.resetAllMocks();
  get.mockImplementation(async (path: string, options: { params?: { query?: { coreId?: string; romSet?: string } } }) => {
    if (path === "/api/v1/runtime/catalog") { return { data: { cores: cores.map(id => ({ id, name: id })),
      bindings: cores.map(coreId => ({ coreId, providerId: "provider", targetId: coreId, contentKinds: ["ARCADE"], romSetSelection: { datSHA256: digest } })),
      providers: [{ providerId: "provider", targets: cores.map(id => ({ id, targetOptionsSchema: { properties: { machine: { type: "string" } } } })) }] }, response: new Response() }; }
    if (path.endsWith("/arcade")) { return { data: { coreId: options.params?.query?.coreId, parentFiles: [], missingParents: [] }, response: new Response() }; }
    return response(options.params?.query?.coreId ?? "fbneo", options.params?.query?.romSet);
  });
});
afterEach(cleanup);

it.each(cores)("%s supports explicit selection, keyboard search and a return to filename matching", async coreId => {
  function Editor() {
    const [value, setValue] = useState<string | undefined>(undefined);
    return <ROMSetSelector gameId="game" coreId={coreId} entryFile="alpha.zip" value={value} datSHA256={digest} onSelect={setValue} />;
  }
  render(<Editor />);
  const input = screen.getByRole("combobox", { name: "ROM 集（DAT）" });
  await waitFor(() => expect(input).toHaveValue("游戏 alpha · alpha"));
  fireEvent.focus(input); fireEvent.keyDown(input, { key: "ArrowDown" }); fireEvent.keyDown(input, { key: "Enter" });
  await waitFor(() => expect(input).toHaveValue("游戏 beta · beta"));
  expect(get.mock.calls.at(-1)?.[1].params.query).toMatchObject({ coreId, romSet: "beta" });
  fireEvent.click(screen.getByRole("button", { name: "按文件名重新匹配" }));
  await waitFor(() => expect(input).toHaveValue("游戏 alpha · alpha"));
  fireEvent.focus(input); fireEvent.change(input, { target: { value: "1990" } });
  expect(screen.queryByRole("option")).not.toBeInTheDocument();
  await waitFor(() => expect(get.mock.calls.at(-1)?.[1].params.query.q).toBe("1990"));
  expect(await screen.findByRole("option", { name: /游戏 beta/ })).toHaveTextContent("beta · 1990 · 厂商");
});

it("retains each core's own draft selection when switching through all DAT cores", async () => {
  let latest: Schema<"RuntimeConfig">;
  const saved: Schema<"RuntimeConfig"> = { content: { kind: "ARCADE", entryFile: "alpha.zip" }, cores: { fbneo: { romSet: "beta" } } };
  function Editor() {
    const [value, setValue] = useState(saved), [coreId, setCoreId] = useState("fbneo"); latest = value;
    return <RuntimeConfigEditor gameId="game" version={1} coreId={coreId} coreIds={cores} value={value} savedValue={saved}
      onChange={setValue} onCoreChange={setCoreId} files={[]} onUploaded={vi.fn()} />;
  }
  render(<Editor />);
  const core = await screen.findByRole("combobox", { name: "运行核心" });
  for (const coreId of cores.slice(1)) {
    fireEvent.change(core, { target: { value: coreId } });
    const input = await screen.findByRole("combobox", { name: "ROM 集（DAT）" });
    await waitFor(() => expect(input).toHaveValue("游戏 alpha · alpha"));
    fireEvent.focus(input); fireEvent.click(await screen.findByRole("option", { name: /游戏 beta/ }));
    await waitFor(() => expect(input).toHaveValue("游戏 beta · beta"));
    expect(screen.getByLabelText("上传父包", { selector: "input" })).toBeDisabled();
  }
  fireEvent.change(core, { target: { value: "fbneo" } });
  await waitFor(() => expect(screen.getByRole("combobox", { name: "ROM 集（DAT）" })).toHaveValue("游戏 beta · beta"));
  for (const coreId of cores) { expect(latest!.cores?.[coreId].romSet).toBe("beta"); }
  expect(screen.queryByRole("textbox", { name: "machine" })).not.toBeInTheDocument();
  expect(screen.getByLabelText("上传父包", { selector: "input" })).toBeEnabled();
});

it("aborts the previous core's read and ignores its late response after a core switch", async () => {
  const old = Promise.withResolvers<ReturnType<typeof response>>();
  let signal: AbortSignal | undefined;
  get.mockImplementation((_path: string, options: { signal: AbortSignal; params: { query: { coreId: string } } }) => {
    if (options.params.query.coreId === "fbneo") { signal = options.signal; return old.promise; }
    return Promise.resolve(response("mame2003"));
  });
  const props = { gameId: "game", entryFile: "alpha.zip", datSHA256: digest, onSelect: vi.fn() };
  const view = render(<ROMSetSelector key="fbneo" {...props} coreId="fbneo" />);
  await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
  view.rerender(<ROMSetSelector key="mame2003" {...props} coreId="mame2003" />);
  expect(signal?.aborted).toBe(true);
  await waitFor(() => expect(screen.getByRole("combobox")).toHaveValue("游戏 alpha · alpha"));
  await act(async () => old.resolve(response("fbneo", "beta")));
  expect(screen.getByRole("combobox")).toHaveValue("游戏 alpha · alpha");
});
