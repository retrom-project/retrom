import {readFileSync} from "node:fs";
import {resolve} from "node:path";
import {act, cleanup, fireEvent, render, screen, waitFor} from "@testing-library/react";
import {afterEach, beforeEach, expect, it, vi} from "vitest";
import {AuthProvider} from "@/features/auth/auth-provider";
import {ToastProvider} from "@/components/toast-provider";
import {PlayerShell} from "./player-shell";
import {loadProviderRuntime} from "./runtime/provider-dispatcher";
import type {RuntimeEventV1, RuntimeStateV1} from "./runtime/contract";
import type {SaveDraft} from "./save-drafts";
import {runFixture, runtimeFixture} from "./player-test-fixture";

vi.mock("next/navigation", () => ({useRouter: () => ({replace: vi.fn()})}));
vi.mock("./runtime/provider-dispatcher", () => ({loadProviderRuntime: vi.fn(), reclaimProviderContentCache: vi.fn()}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => {resolve = done;});
  return {promise, resolve};
}

// Model only the browser storage boundary; the Player's draft and transport code stays real.
function browserStorage() {
  const records = new Map<string, SaveDraft>();
  const db = {close: vi.fn(), transaction: () => {
    let complete = () => {};
    const tx = {oncomplete: null as null | (() => void), objectStore: () => ({
      put: (draft: SaveDraft) => {complete = () => {records.set(draft.id, draft);}; return {result: draft.id};},
      get: (id: string) => ({result: records.get(id)}),
      delete: (id: string) => {complete = () => {records.delete(id);}; return {result: undefined};},
    })};
    queueMicrotask(() => {complete(); tx.oncomplete?.();});
    return tx;
  }};
  vi.stubGlobal("indexedDB", {open: () => {
    const request = {result: db, onsuccess: null as null | (() => void)};
    queueMicrotask(() => request.onsuccess?.());
    return request;
  }});
  return records;
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => {cleanup(); vi.unstubAllGlobals();});

async function player() {
  const records = browserStorage();
  vi.stubGlobal("matchMedia", (query: string) => ({matches: query === "(hover: hover) and (pointer: fine)", addEventListener: vi.fn(), removeEventListener: vi.fn()}));
  const run = runFixture();
  run.envelope = JSON.parse(readFileSync(resolve(process.cwd(), "../api/runtime-provider/v1/fixtures/valid/game-save-restore.json"), "utf8"));
  run.extinfo.checkpointFormat = "fixture-state-v1";
  run.save = {id: "native-save", name: "Existing save", slot: null, version: 7, extinfo: run.extinfo} as NonNullable<typeof run.save>;
  let state: RuntimeStateV1 = "RUNNING";
  let revision = 0;
  let available = false;
  const listeners = new Set<(event: RuntimeEventV1) => void>();
  const emit = (event: RuntimeEventV1) => listeners.forEach(listener => listener(event));
  let exported = deferred<void>();
  let uploaded = deferred<Response>();
  const runtime = runtimeFixture();
  runtime.getState = () => state;
  runtime.subscribe = listener => {listeners.add(listener); return () => {listeners.delete(listener);};};
  runtime.getCheckpointAvailability = () => ({available, reason: available ? null : "UNCHANGED", revision: String(revision)});
  runtime.checkpoint = vi.fn(async request => {
    expect(request?.intent).toBe("EXPORT");
    state = "CHECKPOINTING";
    emit({type: "STATE_CHANGED", previous: "RUNNING", state});
    await exported.promise;
    state = "RUNNING";
    emit({type: "STATE_CHANGED", previous: "CHECKPOINTING", state});
    return {format: run.extinfo.checkpointFormat, bytes: new Uint8Array([revision]), metadata: {revision: String(revision)}};
  });
  runtime.acknowledgeCheckpoint = vi.fn(async () => {
    available = false;
    emit({type: "CHECKPOINT_AVAILABILITY_CHANGED", availability: runtime.getCheckpointAvailability()});
  });
  runtime.exit = vi.fn(async () => {state = "EXITED";});
  vi.mocked(loadProviderRuntime).mockResolvedValue(runtime);
  const writes: {path: string; metadata: Record<string, unknown>}[] = [];
  vi.stubGlobal("fetch", vi.fn(async (input: Request | string, init?: RequestInit) => {
    const path = typeof input === "string" ? input : new URL(input.url).pathname;
    const method = init?.method ?? (typeof input === "string" ? "GET" : input.method);
    if (path === "/api/v1/auth/context") {return Response.json({user: {id: "owner"}, csrfToken: "fixture-csrf"});}
    if (path === `/api/v1/runs/${run.id}` && method === "GET") {return Response.json(run);}
    if (path === "/api/v1/saves/native-save" && method === "PUT") {
      const metadata = JSON.parse(String((init?.body as FormData).get("metadata")));
      writes.push({path, metadata});
      return uploaded.promise;
    }
    if (path.endsWith("/events")) {return Response.json({});}
    throw new Error(`Unexpected request: ${method} ${path}`);
  }));
  const view = render(<AuthProvider><ToastProvider><PlayerShell runId={run.id} returnTo="/library" contentLoading="ON_DEMAND" /></ToastProvider></AuthProvider>);
  await waitFor(() => expect(loadProviderRuntime).toHaveBeenCalledOnce());
  const toolbar = () => view.container.querySelector(".player-toolbar")!;
  await waitFor(() => expect(toolbar()).not.toHaveClass("is-visible"), {timeout: 3500});
  function change() {
    revision++;
    available = true;
    exported = deferred<void>();
    uploaded = deferred<Response>();
    emit({type: "CHECKPOINT_AVAILABILITY_CHANGED", availability: runtime.getCheckpointAvailability()});
  }
  async function exportFiles() {await act(async () => {exported.resolve();});}
  async function uploadFiles(error = false) {
    await act(async () => {uploaded.resolve(error ? Response.json({code: "VERSION_CONFLICT", message: "version conflict"}, {status: 409}) : Response.json({...run.save, version: 7 + revision}));});
  }
  return {...view, records, writes, runtime, toolbar, change, exportFiles, uploadFiles};
}

it("keeps repeated native exports and uploads quiet without revealing the hidden Player toolbar", async () => {
  const p = await player();
  for (let count = 1; count <= 3; count++) {
    await act(async () => {p.change();});
    expect(p.runtime.getState()).toBe("CHECKPOINTING");
    expect(p.toolbar()).not.toHaveClass("is-visible");
    await p.exportFiles();
    await waitFor(() => expect(p.writes).toHaveLength(count));
    expect(p.records.size).toBe(1);
    expect(screen.queryByText("正在同步存档…")).toBeNull();
    expect(screen.queryByRole("button", {name: "重新同步"})).toBeNull();
    await p.uploadFiles();
    await waitFor(() => expect(p.runtime.acknowledgeCheckpoint).toHaveBeenCalledTimes(count));
    expect(p.records.size).toBe(0);
    expect(p.toolbar()).not.toHaveClass("is-visible");
    expect(screen.queryByText("存档已同步。")).toBeNull();
  }
  expect(p.writes.map(write => write.metadata.version)).toEqual([7, 8, 9]);
  expect(new Set(p.writes.map(write => write.metadata.commitId)).size).toBe(3);
});

it("preserves a failed automatic commit and shows recovery, then confirms a manual retry", async () => {
  const p = await player();
  await act(async () => {p.change();});
  await p.exportFiles();
  await p.uploadFiles(true);
  const retry = await screen.findByRole("button", {name: "重新同步"});
  expect(screen.getByText(/存档已在另一个页面更新/, {selector: ".player-save-upload-progress p"})).toBeVisible();
  expect(screen.getByRole("button", {name: "导出备份"})).toBeVisible();
  expect(p.runtime.acknowledgeCheckpoint).not.toHaveBeenCalled();
  expect(p.records.size).toBe(1);
  // A native change must not replace the failed draft or spam a warning.
  await act(async () => {p.change();});
  expect(screen.queryByText(/请先同步或导出已有草稿/)).toBeNull();
  fireEvent.click(retry);
  await waitFor(() => expect(p.writes).toHaveLength(2));
  expect(screen.getByText("正在同步存档…")).toBeVisible();
  await p.uploadFiles();
  expect(await screen.findByText("存档已同步。")).toBeVisible();
  expect(p.runtime.acknowledgeCheckpoint).toHaveBeenCalledOnce();
  expect(p.records.size).toBe(0);
  expect(p.writes[1].metadata).toEqual(p.writes[0].metadata);
});
