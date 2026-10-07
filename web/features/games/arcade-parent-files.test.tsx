import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import { ArcadeParentFiles } from "./arcade-parent-files";

const calls = vi.hoisted(() => ({ get: vi.fn(), upload: vi.fn() }));
vi.mock("@/lib/api/client", async (original) => ({ ...await original<typeof ApiClient>(), api: { GET: calls.get }, upload: calls.upload }));
beforeEach(() => {
  vi.resetAllMocks();
  calls.get.mockResolvedValue({ data: { coreId: "fbneo", parentFiles: [], missingParents: ["1941.zip"] }, response: new Response() });
});
afterEach(cleanup);
function show(onUploaded = vi.fn()) {
  render(<ArcadeParentFiles gameId="child" version={7} coreId="fbneo" entryFile="1941j.zip"
    files={["1941j.zip", "1941.zip"].map((logicalKey) => ({ id: logicalKey, logicalKey, role: "content", sha256: "hash", sizeBytes: 1 }))}
    selected={[]} onChange={vi.fn()} onUploaded={onUploaded} />);
  return onUploaded;
}

it("shows the runtime's exact missing parent name and excludes the game's entry from binding choices", async () => {
  show();
  expect(await screen.findByText("1941.zip", { selector: "li" })).toBeVisible();
  expect(calls.get).toHaveBeenCalledExactlyOnceWith("/api/v1/admin/games/{gameId}/runtime-options/arcade", {
    params: { path: { gameId: "child" }, query: { coreId: "fbneo" } },
  });
  expect(screen.queryByRole("option", { name: "1941j.zip" })).not.toBeInTheDocument();
  expect(screen.getByRole("option", { name: "1941.zip" })).toBeVisible();
});

it("uploads with the current version and core, then reloads the saved detail only after success", async () => {
  const pending = Promise.withResolvers<object>();
  calls.upload.mockReturnValue(pending.promise);
  const changed = show();
  const input = screen.getByLabelText("上传父包", { selector: "input" });
  const file = new File(["zip"], "1941.zip", { type: "application/zip" });
  fireEvent.change(input, { target: { files: [file] } });
  expect(input).toBeDisabled();
  expect(changed).not.toHaveBeenCalled();
  const [url, body] = calls.upload.mock.calls[0];
  expect(url).toBe("/api/v1/admin/games/child/parents");
  expect(body.get("version")).toBe("7"); expect(body.get("coreId")).toBe("fbneo"); expect(body.get("file")).toBe(file);
  await act(async () => pending.resolve({}));
  expect(changed).toHaveBeenCalledTimes(1);
  expect(screen.getByText("1941.zip", { selector: "li" })).toBeVisible();
  expect(screen.getByText("父包已上传并更新运行配置。")).toBeVisible();
});

it("retains the missing file and allows retry after upload rejection", async () => {
  calls.upload.mockRejectedValue(new Error("不是当前游戏需要的父包"));
  const changed = show();
  const input = screen.getByLabelText("上传父包", { selector: "input" });
  fireEvent.change(input, { target: { files: [new File(["zip"], "wrong.zip")] } });
  expect(await screen.findByRole("alert")).toHaveTextContent("不是当前游戏需要的父包");
  expect(changed).not.toHaveBeenCalled();
  expect(input).toBeEnabled();
  expect(screen.getByText("1941.zip", { selector: "li" })).toBeVisible();
});

it("keeps read failures separate from an empty missing-parent result and retries the check", async () => {
  calls.get.mockRejectedValueOnce(new Error("检查暂不可用"));
  show();
  expect(await screen.findByRole("alert")).toHaveTextContent("无法检查父包：检查暂不可用");
  expect(screen.queryByText("已保存配置未发现缺失父包。")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "重新检查父包" }));
  await waitFor(() => expect(screen.getByText("1941.zip", { selector: "li" })).toBeVisible());
});
