import { MessageChannel } from "node:worker_threads";
import { afterEach, expect, test, vi } from "vitest";
import { activateUpdate, observeWorker } from "./worker-client";

afterEach(() => vi.unstubAllGlobals());
function environment(status: "playing" | "activating") {
  const container = Object.assign(new EventTarget(), { controller: null as unknown });
  const worker = { postMessage: vi.fn((_message: unknown, ports: MessagePort[]) => {
    ports[0].postMessage({ status });
    if (status === "activating") {
      container.controller = worker;
      container.dispatchEvent(new Event("controllerchange"));
    }
  }) };
  const registration = Object.assign(new EventTarget(), { waiting: worker, active: {}, installing: null, update: vi.fn(async () => {}) });
  const register = vi.fn(async () => registration);
  Object.assign(container, { register });
  vi.stubGlobal("navigator", { serviceWorker: container });
  vi.stubGlobal("MessageChannel", MessageChannel);
  return { worker, register, registration: registration as unknown as ServiceWorkerRegistration };
}
test("observing a waiting update never activates it or reloads a window", async () => {
  const app = environment("activating");
  const changed = vi.fn();
  const observer = observeWorker(changed);
  await vi.waitFor(() => expect(changed).toHaveBeenCalledWith({ ready: true, update: true, error: "" }));
  expect(app.worker.postMessage).not.toHaveBeenCalled();
  expect(app.register).toHaveBeenCalledWith("/sw.js", { scope: "/", updateViaCache: "none" });
  observer.dispose();
});
test("an explicit update handles a controller change that precedes the acknowledgement", async () => {
  const app = environment("activating");
  expect(await activateUpdate(app.registration)).toBe("updated");
});
test("another active game keeps the update waiting", async () => {
  const app = environment("playing");
  expect(await activateUpdate(app.registration)).toBe("playing");
});
test("a registration failure is visible and can be retried", async () => {
  const app = environment("activating");
  app.register.mockRejectedValueOnce(new TypeError("offline"));
  const changed = vi.fn();
  const observer = observeWorker(changed);
  await vi.waitFor(() => expect(changed).toHaveBeenCalledWith(expect.objectContaining({ ready: false, error: expect.stringContaining("恢复连接") })));
  await observer.check();
  expect(changed).toHaveBeenLastCalledWith({ ready: true, update: true, error: "" });
  observer.dispose();
});
