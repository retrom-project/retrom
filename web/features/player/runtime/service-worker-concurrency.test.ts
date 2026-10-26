// @vitest-environment node
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { createContext, runInContext } from "node:vm";
import { expect, it, vi } from "vitest";

type Message = { id: number; type: string; path: string };
type Port = {
  onmessage: ((event: { data: unknown }) => void) | null;
  postMessage: (message: Message) => void;
  start: () => void;
  close: () => void;
};

function deferredContentBridge() {
  const requests: Array<{ message: Message; port: Port }> = [];
  const bytes = new TextEncoder().encode("window.pluginLoaded = true;");
  let sendFailures = 0;
  function createPort(): Port {
    const port: Port = {
      onmessage: null,
      postMessage(message) {
        if (sendFailures) {
          sendFailures--;
          throw new Error("PORT_SEND_FAILED");
        }
        requests.push({ message, port });
      },
      start() {},
      close() {},
    };
    return port;
  }
  const context = createContext({
    self: {
      location: {
        href: "http://isolated.test/__retrom/runtime-isolation/run/service-worker.js",
      },
      addEventListener() {},
    },
    URL, Headers, Response, ReadableStream, TextEncoder, TextDecoder, Uint8Array,
    setTimeout, clearTimeout,
  });
  runInContext(readFileSync(fileURLToPath(new URL(
    "../../../public/runtime-isolation/service-worker.js", import.meta.url,
  )), "utf8"), context);
  const bind = context.bind as (port: unknown, config: unknown) => void;
  const config = { entryFile: "index.html", parentOrigin: "http://host.test" };
  bind(createPort(), config);
  return {
    serve: context.serve as (request: Request, path: string) => Promise<Response>,
    request: context.request as (message: Omit<Message, "id">) => Promise<unknown>,
    rebind: () => bind(createPort(), config),
    failNextSend: () => { sendFailures++; },
    flush() {
      const batch = requests.splice(0);
      for (const { message, port } of batch) {
        port.onmessage?.({ data: message.type === "STAT"
          ? { id: message.id, status: 200, path: message.path,
              sizeBytes: bytes.length, mediaType: "text/javascript" }
          : { id: message.id, status: 200, bytes: bytes.slice() } });
      }
      return batch.length;
    },
    pending: () => requests.length,
    notify(index: number, type: string, networkBytes?: number) {
      const {message, port} = requests[index];
      port.onmessage?.({data: {id: message.id, type, networkBytes}});
    },
  };
}

it("loads 148 simultaneous plugin scripts without rejecting excess bridge requests", async () => {
  const bridge = deferredContentBridge();
  const results = Promise.allSettled(Array.from({ length: 148 }, (_, index) => {
    const path = `js/plugins/plugin-${index}.js`;
    return bridge.serve(new Request(`http://isolated.test/run/run/${path}`), path)
      .then(async (response) => ({ status: response.status, text: await response.text() }));
  }));
  let finished = false;
  void results.then(() => { finished = true; });
  for (let turn = 0; turn < 100 && !finished; turn++) {
    expect(bridge.pending()).toBeLessThanOrEqual(128);
    bridge.flush();
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  const settled = await results;
  expect(settled.filter((result) => result.status === "rejected")).toHaveLength(0);
  expect(settled).toHaveLength(148);
  for (const result of settled) {
    expect(result).toEqual({ status: "fulfilled", value: {
      status: 200, text: "window.pluginLoaded = true;",
    } });
  }
  expect(bridge.pending()).toBe(0);
});

it("renews a slow read on advancing data and keeps queued reads alive while admission makes progress", async () => {
  vi.useFakeTimers();
  try {
    const bridge = deferredContentBridge();
    const pending = [bridge.request({type: "READ", path: "slow"}), bridge.request({type: "READ", path: "queued"})];
    const result = Promise.allSettled(pending);
    await vi.advanceTimersByTimeAsync(0);
    bridge.notify(0, "READ_STARTED");
    for (let n = 1; n <= 4; n++) {
      await vi.advanceTimersByTimeAsync(8000);
      bridge.notify(0, "READ_PROGRESS", n * 65536);
      expect(bridge.pending()).toBe(2);
    }
    bridge.notify(1, "READ_STARTED"); bridge.flush();
    expect((await result).every(item => item.status === "fulfilled")).toBe(true);
  } finally {vi.useRealTimers();}
});

it("does not renew a stalled active read on duplicate notifications or another read's progress", async () => {
  vi.useFakeTimers();
  try {
    const bridge = deferredContentBridge();
    const pending = [bridge.request({type: "READ", path: "slow"}), bridge.request({type: "READ", path: "stalled"})];
    const result = Promise.allSettled(pending);
    await vi.advanceTimersByTimeAsync(0);
    bridge.notify(0, "READ_STARTED"); bridge.notify(1, "READ_STARTED");
    bridge.notify(1, "READ_PROGRESS", 65536);
    for (let n = 1; n <= 4; n++) {
      await vi.advanceTimersByTimeAsync(4000);
      bridge.notify(0, "READ_PROGRESS", n * 65536);
      bridge.notify(1, "READ_STARTED"); bridge.notify(1, "READ_PROGRESS", 65536);
    }
    bridge.flush();
    const settled = await result;
    expect(settled[0].status).toBe("fulfilled");
    expect(settled[1]).toMatchObject({status: "rejected", reason: {message: "CONTENT_IO_TIMEOUT"}});
  } finally {vi.useRealTimers();}
});

it("keeps the backlog bounded and admits later requests after it drains", async () => {
  const bridge = deferredContentBridge();
  const results = Promise.allSettled(Array.from({ length: 400 }, (_, index) =>
    bridge.request({ type: "STAT", path: `js/plugins/plugin-${index}.js` })));
  let finished = false;
  void results.then(() => { finished = true; });
  for (let turn = 0; turn < 100 && !finished; turn++) {
    expect(bridge.pending()).toBeLessThanOrEqual(128);
    bridge.flush();
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  const settled = await results;
  expect(settled.filter((result) => result.status === "fulfilled")).toHaveLength(384);
  const rejected = settled.filter((result) => result.status === "rejected");
  expect(rejected).toHaveLength(16);
  for (const result of rejected) {
    expect(result.reason.message).toBe("CONTENT_IO_CAPACITY_EXCEEDED");
  }
  const later = bridge.request({ type: "STAT", path: "js/plugins/later.js" });
  await new Promise((resolve) => setTimeout(resolve, 0));
  bridge.flush();
  await expect(later).resolves.toMatchObject({ status: 200 });
});

it("releases transport slots and timers after a port send fails", async () => {
  const bridge = deferredContentBridge();
  bridge.failNextSend();
  const results = Promise.allSettled(Array.from({ length: 148 }, (_, index) =>
    bridge.request({ type: "STAT", path: `js/plugins/plugin-${index}.js` })));
  let finished = false;
  void results.then(() => { finished = true; });
  for (let turn = 0; turn < 100 && !finished; turn++) {
    bridge.flush();
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  const settled = await results;
  expect(settled.filter((result) => result.status === "fulfilled")).toHaveLength(147);
  expect(settled.filter((result) => result.status === "rejected"))
    .toEqual([{ status: "rejected", reason: expect.objectContaining({ message: "PORT_SEND_FAILED" }) }]);
  expect(bridge.pending()).toBe(0);
});

it("preserves a replacement port when old requests time out and dispatches queued requests", async () => {
  vi.useFakeTimers();
  try {
    const bridge = deferredContentBridge();
    const results = Promise.allSettled(Array.from({ length: 148 }, (_, index) =>
      bridge.request({ type: "STAT", path: `js/plugins/plugin-${index}.js` })));
    await vi.advanceTimersByTimeAsync(0);
    expect(bridge.pending()).toBe(128);
    bridge.rebind();
    await vi.advanceTimersByTimeAsync(15_000);
    bridge.flush();
    const settled = await results;
    expect(settled.filter((result) => result.status === "fulfilled")).toHaveLength(20);
    const rejected = settled.filter((result) => result.status === "rejected");
    expect(rejected).toHaveLength(128);
    for (const result of rejected) {
      expect(result.reason.message).toBe("CONTENT_IO_TIMEOUT");
    }
    expect(bridge.pending()).toBe(0);
  } finally {
    vi.useRealTimers();
  }
});
