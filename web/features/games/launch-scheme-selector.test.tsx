import {useState} from "react";
import {act, cleanup, fireEvent, render, screen, waitFor} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import type * as ApiClient from "@/lib/api/client";
import type {Schema} from "@/lib/api/types";
import {LaunchSchemeSelector} from "./launch-scheme-selector";

const post = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client", async original => ({...await original<typeof ApiClient>(), api: {POST: post}}));
afterEach(() => {cleanup(); post.mockReset();});
const base: Schema<"RuntimeConfig"> = {content: {kind: "KIRIKIRI_PROJECT", entryFile: "data.xp3"},
  cores: {kirikiri2: {options: {engineArguments: ["-caption=Edited"]}}}};
function reply(config = base) {
  return {data: {selectedId: "original", items: [
    {id: "original", label: "原版资源", configuration: config},
    {id: "patches/dialogue", label: "资源补丁：loader.dll", configuration: {...config,
      cores: {kirikiri2: {options: {...config.cores?.kirikiri2?.options, overlayRoot: "patches/dialogue"}}}}},
  ]}, response: new Response()};
}
function Selector({gameId = "game"}: {gameId?: string}) {
  const [value, onChange] = useState(base);
  return <><LaunchSchemeSelector gameId={gameId} value={value} onChange={onChange} onAvailable={vi.fn()} />
    <output data-testid="configuration">{JSON.stringify(value)}</output></>;
}
it("uses runtime launch schemes while retaining the draft entry and other options", async () => {
  post.mockImplementation(async (_path, input) => reply(input.body));
  render(<Selector />);
  fireEvent.change(await screen.findByRole("combobox", {name: "启动方案"}), {target: {value: "patches/dialogue"}});
  expect(JSON.parse(screen.getByTestId("configuration").textContent ?? "{}")).toEqual({
    ...base, cores: {kirikiri2: {options: {engineArguments: ["-caption=Edited"], overlayRoot: "patches/dialogue"}}},
  });
  await waitFor(() => expect(post).toHaveBeenCalledWith("/api/v1/admin/games/{gameId}/runtime-options/launches",
    expect.objectContaining({params: {path: {gameId: "game"}}, body: expect.objectContaining({content: base.content})})));
});
it("keeps failed discovery distinct from a project with no recognized patches", async () => {
  post.mockRejectedValue(new Error("unavailable"));
  const available = vi.fn();
  render(<LaunchSchemeSelector gameId="game" value={base} onChange={vi.fn()} onAvailable={available} />);
  expect(await screen.findByRole("alert")).toHaveTextContent("无法读取启动方案");
  post.mockResolvedValue({data: {items: [], selectedId: ""}, response: new Response()});
  fireEvent.click(screen.getByRole("button", {name: "重试读取启动方案"}));
  await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  expect(available).toHaveBeenLastCalledWith(false);
});
it("discards delayed launch schemes after switching games", async () => {
  let finish!: (value: ReturnType<typeof reply>) => void;
  post.mockImplementationOnce(() => new Promise(resolve => {finish = resolve;}));
  const view = render(<Selector />);
  await waitFor(() => expect(finish).toBeDefined());
  post.mockResolvedValue({data: {items: [], selectedId: ""}, response: new Response()});
  view.rerender(<Selector gameId="other" />);
  await act(async () => {finish(reply());});
  expect(screen.queryByRole("combobox", {name: "启动方案"})).not.toBeInTheDocument();
});
