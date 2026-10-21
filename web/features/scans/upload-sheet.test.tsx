import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { GameScan } from "./game-scan";

const calls = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), delete: vi.fn(), phone: { value: false } }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: (path: string) => window.history.pushState(null, "", path) }) }));
vi.mock("@/lib/use-phone-layout", () => ({ usePhoneLayout: () => calls.phone.value }));
vi.mock("@/lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiClient>();
  return { ...actual, api: { GET: calls.get, POST: calls.post, DELETE: calls.delete } };
});

const uploadId = "4f0a7d1e-62b4-4a43-9a58-8e5f21b5c7a1";
const fetchMock = vi.fn<typeof fetch>();
let pending: Schema<"Upload">[] = [];
function pendingUpload(completed: boolean): Schema<"Upload"> {
  return {
    id: uploadId, format: "files", chunkSizeBytes: 64 * 1024 * 1024, expiresAtMs: Date.UTC(2026, 9, 23), completed,
    files: [{ path: "Super Game.nes", sizeBytes: 14, receivedBytes: completed ? 14 : 0 }, { path: "Other.nes", sizeBytes: 9, receivedBytes: 9 }],
  };
}
beforeEach(() => {
  pending = [];
  vi.resetAllMocks();
  calls.phone.value = false;
  window.localStorage.clear();
  vi.stubGlobal("fetch", fetchMock);
  window.history.replaceState(null, "", "/admin/imports/server");
  calls.get.mockImplementation((path: string) => Promise.resolve({
    data: path === "/api/v1/admin/uploads/{uploadId}" ? pending[0]
      : { items: path === "/api/v1/platform-instances" ? [{ id: "directory", name: "NES", enabled: true }]
        : path === "/api/v1/admin/uploads" ? pending : [] },
    response: new Response(),
  }));
  calls.post.mockImplementation((path: string, init: { body: Record<string, unknown> }) => {
    const responses: Record<string, () => unknown> = {
      "/api/v1/admin/uploads": () => ({
        id: uploadId, chunkSizeBytes: 64 * 1024 * 1024, expiresAtMs: 1, completed: false,
        files: (init.body.files as { path: string; sizeBytes: number }[]).map((file) => ({ ...file, receivedBytes: 0 })),
      }),
      "/api/v1/admin/game-scans/inspect": () => ({ items: [{ key: ".", name: "files", path: "/staging/files", gameCount: 2 }] }),
      "/api/v1/admin/game-scans": () => ({ id: "scan" }),
    };
    return Promise.resolve({ data: responses[path](), response: new Response() });
  });
  fetchMock.mockImplementation((_, init) => Promise.resolve(new Response(
    JSON.stringify({ receivedBytes: (init?.body as Blob).size, completed: true }), { status: 200 })));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function chooseFiles(names: string[]) {
  const input = screen.getByText("选择文件").querySelector("input") as HTMLInputElement;
  fireEvent.change(input, { target: { files: names.map((name) => new File([name], name)) } });
}

it("uploads loose files, reads them as one collection and scans the upload instead of a server path", async () => {
  render(<GameScan />);
  fireEvent.click(screen.getByRole("button", { name: "浏览器上传" }));
  const start = screen.getByRole("button", { name: "开始上传" });
  expect(start).toBeDisabled();
  fireEvent.change(screen.getByRole("combobox", { name: "上传文件组织形式" }), { target: { value: "files" } });
  expect(screen.getByText(/同名的 png\/jpg\/webp 图片作为封面/u)).toBeInTheDocument();
  chooseFiles(["Super Game.nes", "Super Game.png", "Other.nes"]);
  expect(screen.getByRole("region", { name: "已选择的文件" })).toHaveTextContent("3 个文件");
  fireEvent.click(start);
  const directory = await screen.findByRole("combobox", { name: "上传的游戏文件游戏目录" });
  expect(calls.post).toHaveBeenCalledWith("/api/v1/admin/uploads", {
    body: { format: "files", files: [
      { path: "Other.nes", sizeBytes: 9 }, { path: "Super Game.nes", sizeBytes: 14 }, { path: "Super Game.png", sizeBytes: 14 },
    ] },
  });
  expect(fetchMock).toHaveBeenCalledTimes(3);
  expect(calls.post).toHaveBeenCalledWith("/api/v1/admin/game-scans/inspect", { body: { uploadId, format: "files" } });
  expect(screen.getByText(/浏览器上传 · 3 个文件/u)).toBeInTheDocument();
  expect(screen.queryByText(/\/staging\/files/u)).not.toBeInTheDocument();
  fireEvent.change(directory, { target: { value: "directory" } });
  fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
  await waitFor(() => expect(window.location.search).toBe("?scanId=scan"));
  expect(calls.post).toHaveBeenLastCalledWith("/api/v1/admin/game-scans", {
    body: { uploadId, format: "files", mappings: [{ sourceKey: ".", platformInstanceId: "directory", tagIds: [] }] },
  });
});

it("cancels a started upload and blocks selections above the limits", async () => {
  fetchMock.mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ code: "INTERNAL_ERROR", message: "失败" }), { status: 400 })));
  calls.delete.mockResolvedValue({ data: undefined, response: new Response(null, { status: 204 }) });
  render(<GameScan />);
  fireEvent.click(screen.getByRole("button", { name: "浏览器上传" }));
  fireEvent.change(screen.getByRole("combobox", { name: "上传文件组织形式" }), { target: { value: "pegasus" } });
  chooseFiles(["metadata.pegasus.txt"]);
  fireEvent.click(screen.getByRole("button", { name: "开始上传" }));
  expect(await screen.findByText("失败")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "继续上传" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "取消上传" }));
  await waitFor(() => expect(calls.delete).toHaveBeenCalledWith("/api/v1/admin/uploads/{uploadId}", {
    params: { path: { uploadId } },
  }));
  expect(screen.queryByRole("dialog", { name: "浏览器上传游戏" })).not.toBeInTheDocument();
});

it("keeps browser uploads off phones", () => {
  calls.phone.value = true;
  render(<GameScan />);
  expect(screen.queryByRole("button", { name: "浏览器上传" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "本地扫描" })).toBeInTheDocument();
});

it("maps a finished upload after a reload without uploading it again", async () => {
  pending = [pendingUpload(true)];
  render(<GameScan />);
  const panel = await screen.findByRole("region", { name: "未完成的上传" });
  expect(panel).toHaveTextContent("2 个文件 · 散装文件");
  expect(panel).toHaveTextContent("待映射目录");
  fireEvent.click(screen.getByRole("button", { name: "继续映射" }));
  const directory = await screen.findByRole("combobox", { name: "上传的游戏文件游戏目录" });
  expect(calls.post).toHaveBeenCalledExactlyOnceWith("/api/v1/admin/game-scans/inspect", { body: { uploadId, format: "files" } });
  expect(fetchMock).not.toHaveBeenCalled();
  fireEvent.change(directory, { target: { value: "directory" } });
  fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
  await waitFor(() => expect(calls.post).toHaveBeenLastCalledWith("/api/v1/admin/game-scans", {
    body: { uploadId, format: "files", mappings: [{ sourceKey: ".", platformInstanceId: "directory", tagIds: [] }] },
  }));
});

it("resumes an unfinished upload only with its original files and sends just the missing bytes", async () => {
  pending = [pendingUpload(false)];
  render(<GameScan />);
  fireEvent.click(await screen.findByRole("button", { name: "继续上传" }));
  const sheet = screen.getByRole("dialog", { name: "浏览器上传游戏" });
  expect(sheet).toHaveTextContent("重新选择原来的 2 个文件");
  expect(screen.getByRole("combobox", { name: "上传文件组织形式" })).toHaveValue("files");
  chooseFiles(["Super Game.nes"]);
  expect(sheet).toHaveTextContent("路径和大小需与上传时一致");
  expect(within(sheet).getByRole("button", { name: "继续上传" })).toBeDisabled();
  fireEvent.click(within(sheet).getByRole("button", { name: "清空" }));
  chooseFiles(["Super Game.nes", "Other.nes"]);
  fireEvent.click(within(sheet).getByRole("button", { name: "继续上传" }));
  await screen.findByRole("combobox", { name: "上传的游戏文件游戏目录" });
  expect(calls.post).not.toHaveBeenCalledWith("/api/v1/admin/uploads", expect.anything());
  expect(fetchMock).toHaveBeenCalledTimes(1);
  expect(String(fetchMock.mock.calls[0][0])).toContain(`/api/v1/admin/uploads/${uploadId}/files/0?offset=0`);
});

it("releases an abandoned upload from the list", async () => {
  pending = [pendingUpload(false)];
  calls.delete.mockResolvedValue({ data: undefined, response: new Response(null, { status: 204 }) });
  render(<GameScan />);
  fireEvent.click(await screen.findByRole("button", { name: "放弃" }));
  await waitFor(() => expect(calls.delete).toHaveBeenCalledWith("/api/v1/admin/uploads/{uploadId}", {
    params: { path: { uploadId } },
  }));
  expect(await screen.findByText("已放弃上传并删除暂存文件。")).toBeInTheDocument();
});

it("uploads a picked folder of ROMs as separate games unless the folder is one game", async () => {
  render(<GameScan />);
  fireEvent.click(screen.getByRole("button", { name: "浏览器上传" }));
  fireEvent.change(screen.getByRole("combobox", { name: "上传文件组织形式" }), { target: { value: "files" } });
  const folderInput = screen.getByText("选择文件夹").querySelector("input") as HTMLInputElement;
  const files = ["X地带.zip", "X地带.png", "Other.nes"].map((name) => {
    const file = new File([name], name);
    Object.defineProperty(file, "webkitRelativePath", { value: `test/${name}` });
    return file;
  });
  fireEvent.change(folderInput, { target: { files } });
  const result = screen.getByRole("region", { name: "识别结果" });
  const eachFile = screen.getByRole("checkbox", { name: "把“test”文件夹里的每个文件作为一款游戏" });
  expect(eachFile).toBeChecked();
  expect(result).toHaveTextContent("将识别为 2 款游戏：Other、X地带");
  fireEvent.click(eachFile);
  expect(result).toHaveTextContent("将识别为 1 款游戏：test");
  fireEvent.click(eachFile);
  fireEvent.click(screen.getByRole("button", { name: "开始上传" }));
  await screen.findByRole("combobox", { name: "上传的游戏文件游戏目录" });
  expect(calls.post).toHaveBeenCalledWith("/api/v1/admin/uploads", { body: { format: "files", files: [
    { path: "Other.nes", sizeBytes: 9 }, { path: "X地带.png", sizeBytes: 11 }, { path: "X地带.zip", sizeBytes: 11 },
  ] } });
});
