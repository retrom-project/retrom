import { useState } from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { api } from "@/lib/api/client";
import { SourcePicker } from "./source-picker";

vi.mock("@/lib/api/client", () => ({
  api: { GET: vi.fn() },
  result: (response: { data: unknown }) => response.data,
}));
afterEach(cleanup);
function Picker() {
  const [path, setPath] = useState("/");
  const [pending, setPending] = useState(false);
  return <><SourcePicker path={path} onChange={setPath} onPendingChange={setPending} /><button disabled={pending}>开始扫描</button></>;
}
const getDirectories = api.GET<"/api/v1/admin/source-directories", { params: { query: { path: string } } }>;
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(getDirectories).mockResolvedValue({ data: { items: [{ name: "games", path: "/games" }] }, response: new Response() });
});
it("keeps typed paths as drafts until Enter and prevents scanning a previous selection", async () => {
  render(<Picker />);
  await screen.findByRole("button", { name: "games" });
  expect(api.GET).toHaveBeenCalledTimes(1);
  fireEvent.change(screen.getByLabelText("服务器目录"), { target: { value: "/library" } });
  expect(api.GET).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("button", { name: "开始扫描" })).toBeDisabled();
  fireEvent.submit(screen.getByRole("button", { name: "进入目录" }).closest("form")!);
  await waitFor(() => expect(api.GET).toHaveBeenLastCalledWith("/api/v1/admin/source-directories", { params: { query: { path: "/library" } } }));
  expect(screen.getByRole("button", { name: "开始扫描" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "上级目录" }));
  await waitFor(() => expect(api.GET).toHaveBeenLastCalledWith("/api/v1/admin/source-directories", { params: { query: { path: "/" } } }));
  expect(screen.getByLabelText("服务器目录")).toHaveValue("/");
  expect(screen.getByRole("button", { name: "上级目录" })).toBeDisabled();
});
it("navigates directory entries using their absolute path and does not request a relative draft", async () => {
  render(<Picker />);
  fireEvent.click(await screen.findByRole("button", { name: "games" }));
  await waitFor(() => expect(api.GET).toHaveBeenLastCalledWith("/api/v1/admin/source-directories", { params: { query: { path: "/games" } } }));
  expect(screen.getByLabelText("服务器目录")).toHaveValue("/games");
  const calls=vi.mocked(api.GET).mock.calls.length;
  fireEvent.change(screen.getByLabelText("服务器目录"), { target: { value: "relative" } });
  expect(screen.getByRole("button", { name: "进入目录" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "开始扫描" })).toBeDisabled();
  expect(api.GET).toHaveBeenCalledTimes(calls);
});
