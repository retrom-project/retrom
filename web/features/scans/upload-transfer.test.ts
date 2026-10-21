import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type * as ApiClient from "@/lib/api/client";
import { inputEntries, looksLikeLooseFolder, looseTitles, mergeEntries, singleFolder, unwrapFolder } from "./upload-files";
import { transferUpload, uploadLimits, uploadProblem, type UploadEntry, type UploadSession } from "./upload-transfer";

const calls = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("@/lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiClient>();
  return { ...actual, api: { GET: calls.get } };
});

const fetchMock = vi.fn<typeof fetch>();
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function entry(path: string, size: number): UploadEntry {
  const file = new File([new Uint8Array(Math.min(size, 16))], path.split("/").pop() ?? path);
  Object.defineProperty(file, "size", { value: size });
  return { path, file };
}

function session(files: UploadEntry[], chunkSizeBytes: number, received: number[] = []): UploadSession {
  return {
    id: "4f0a7d1e-62b4-4a43-9a58-8e5f21b5c7a1", format: "files", chunkSizeBytes, expiresAtMs: 1, completed: false,
    files: files.map((item, index) => ({ path: item.path, sizeBytes: item.file.size, receivedBytes: received[index] ?? 0 })),
  };
}

function progress(receivedBytes: number) {
  return new Response(JSON.stringify({ receivedBytes, completed: false }), { status: 200 });
}

it("checks the same limits as the server before declaring an upload", () => {
  expect(uploadProblem([])).toContain("请选择");
  expect(uploadProblem([entry("Quest/Game.exe", 10), entry("rom.nes", 0)])).toBe("");
  expect(uploadProblem([entry("disc.iso", uploadLimits.fileBytes + 1)])).toContain("4 GiB");
  expect(uploadProblem(Array.from({ length: 6 }, (_, index) => entry(`${index}.iso`, uploadLimits.fileBytes))))
    .toContain("20 GiB");
  expect(uploadProblem([entry("a", 1), entry("a/b.nes", 1)])).toContain("同时是文件和文件夹");
  expect(uploadProblem([entry("../a.nes", 1)])).toContain("路径不能上传");
  expect(uploadProblem([entry(`${"d/".repeat(25)}a.nes`, 1)])).toContain("路径不能上传");
});

it("keeps folder names from folder inputs and lets a reselected path replace the earlier file", () => {
  const folderFile = new File(["x"], "Game.exe");
  Object.defineProperty(folderFile, "webkitRelativePath", { value: "Quest/Game.exe" });
  const list = { 0: folderFile, length: 1, item: () => folderFile, [Symbol.iterator]: [folderFile][Symbol.iterator].bind([folderFile]) };
  const [picked] = inputEntries(list as unknown as FileList);
  expect(picked.path).toBe("Quest/Game.exe");
  const replacement = entry("Quest/Game.exe", 3);
  expect(mergeEntries([picked, entry("a.nes", 1)], [replacement]).map((item) => [item.path, item.file.size])).toEqual([["a.nes", 1], ["Quest/Game.exe", 3]]);
});

it("sends every missing byte in chunks at the received offset and resumes after a conflict", async () => {
  const files = [entry("big.bin", 10), entry("done.nes", 4)];
  const offsets: string[] = [];
  fetchMock.mockImplementation((url) => {
    const target = new URL(String(url), "http://retrom.test");
    offsets.push(`${target.pathname.split("/").pop()}@${target.searchParams.get("offset")}`);
    const offset = Number(target.searchParams.get("offset"));
    if (offsets.length === 2) { return Promise.resolve(new Response(JSON.stringify({ code: "UPLOAD_OFFSET_CONFLICT" }), { status: 409 })); }
    return Promise.resolve(progress(Math.min(10, offset + 4)));
  });
  // After the conflict the server reports it already holds 8 bytes, e.g. from an earlier attempt.
  calls.get.mockResolvedValue({ data: session(files, 4, [8, 4]), response: new Response() });
  const reports: number[] = [];
  await transferUpload(session(files, 4, [0, 4]), files, (totals) => reports.push(totals.receivedBytes),
    new AbortController().signal);
  expect(offsets).toEqual(["0@0", "0@4", "0@8"]);
  expect(reports.at(-1)).toBe(14);
  const body = fetchMock.mock.calls[0][1]?.body as Blob;
  expect(body.size).toBe(4);
  expect(fetchMock.mock.calls[0][1]?.headers).toMatchObject({ "Content-Type": "application/octet-stream" });
});

it("retries a chunk after a network failure or server error and stops on a client error", async () => {
  vi.useFakeTimers();
  const files = [entry("rom.nes", 4)];
  fetchMock
    .mockRejectedValueOnce(new TypeError("network down"))
    .mockResolvedValueOnce(new Response("{}", { status: 503 }))
    .mockResolvedValueOnce(progress(4));
  const done = transferUpload(session(files, 4), files, () => undefined, new AbortController().signal);
  await vi.runAllTimersAsync();
  await expect(done).resolves.toBeUndefined();
  expect(fetchMock).toHaveBeenCalledTimes(3);

  fetchMock.mockReset();
  fetchMock.mockResolvedValue(new Response(JSON.stringify({ code: "NOT_FOUND", message: "Item is unavailable" }), { status: 404 }));
  await expect(transferUpload(session(files, 4), files, () => undefined, new AbortController().signal))
    .rejects.toMatchObject({ status: 404 });
  expect(fetchMock).toHaveBeenCalledTimes(1);
});

it("treats one folder of plain files as loose games and a folder with subfolders as one game", () => {
  const roms = [entry("test/X地带.zip", 3), entry("test/X地带.png", 2), entry("test/Other.nes", 1)];
  expect(singleFolder(roms)).toBe("test");
  expect(looksLikeLooseFolder(roms, "test")).toBe(true);
  expect(looseTitles(roms)).toEqual(["test"]);
  expect(looseTitles(unwrapFolder(roms, "test"))).toEqual(["Other", "X地带"]);
  const rpg = [entry("Quest/Game.exe", 3), entry("Quest/www/data/Map001.json", 1)];
  expect(looksLikeLooseFolder(rpg, "Quest")).toBe(false);
  expect(singleFolder([...rpg, entry("Other.nes", 1)])).toBe("");
  expect(looseTitles([entry("Lonely.webp", 1), entry(".DS_Store", 1)])).toEqual(["Lonely"]);
});
