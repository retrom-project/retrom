export type WorkerState = { ready: boolean; update: boolean; error: string };
const unavailable = "暂时无法准备离线提示，请恢复连接后重试。";

export function observeWorker(changed: (state: WorkerState) => void) {
  let registration: ServiceWorkerRegistration | undefined;
  let disposed = false;
  let lastCheck = 0;
  const cleanups: Array<() => void> = [];
  const publish = () => {
    if (!disposed && registration) {
      changed({ ready: Boolean(registration.active), update: Boolean(registration.waiting), error: "" });
    }
  };
  const failure = () => {
    if (!disposed) { changed({ ready: Boolean(registration?.active), update: Boolean(registration?.waiting), error: unavailable }); }
  };
  function watchInstalling() {
    const worker = registration?.installing;
    if (!worker) { return; }
    const stateChanged = () => {
      if (worker.state === "redundant") { failure(); } else { publish(); }
    };
    worker.addEventListener("statechange", stateChanged);
    cleanups.push(() => worker.removeEventListener("statechange", stateChanged));
  }
  async function check() {
    if (disposed) { return; }
    lastCheck = Date.now();
    try {
      if (!registration) {
        const value = await navigator.serviceWorker.register("/sw.js", { scope: "/", updateViaCache: "none" });
        if (disposed) { return; }
        registration = value;
        registration.addEventListener("updatefound", watchInstalling);
        cleanups.push(() => value.removeEventListener("updatefound", watchInstalling));
        watchInstalling();
      } else {
        await registration.update();
      }
      publish();
    } catch { failure(); }
  }
  function visible() {
    if (document.visibilityState === "visible" && Date.now() - lastCheck > 60_000) { void check(); }
  }
  const online = () => { void check(); };
  window.addEventListener("online", online);
  window.addEventListener("focus", visible);
  document.addEventListener("visibilitychange", visible);
  navigator.serviceWorker.addEventListener("controllerchange", publish);
  void check();
  return {
    check,
    apply: () => activateUpdate(registration),
    dispose() {
      disposed = true;
      cleanups.forEach(cleanup => cleanup());
      window.removeEventListener("online", online);
      window.removeEventListener("focus", visible);
      document.removeEventListener("visibilitychange", visible);
      navigator.serviceWorker.removeEventListener("controllerchange", publish);
    },
  };
}

export function activateUpdate(registration?: ServiceWorkerRegistration): Promise<"updated" | "playing"> {
  const worker = registration?.waiting;
  if (!worker) { return Promise.reject(new Error("暂时没有可更新的版本，请重新检查。")); }
  return new Promise((resolve, reject) => {
    const channel = new MessageChannel();
    let approved = false;
    let controlled = false;
    const timer = setTimeout(() => finish(new Error("更新尚未完成，请稍后重试。")), 10_000);
    function finish(value: "updated" | "playing" | Error) {
      clearTimeout(timer);
      navigator.serviceWorker.removeEventListener("controllerchange", controllerChanged);
      channel.port1.close();
      channel.port2.close();
      if (value instanceof Error) { reject(value); } else { resolve(value); }
    }
    function controllerChanged() {
      controlled = true;
      if (approved) { finish("updated"); }
    }
    channel.port1.onmessage = ({ data }: MessageEvent<{ status?: string }>) => {
      if (data?.status === "playing") { finish("playing"); }
      if (data?.status === "activating") {
        approved = true;
        if (controlled || navigator.serviceWorker.controller === worker) { finish("updated"); }
      }
    };
    navigator.serviceWorker.addEventListener("controllerchange", controllerChanged);
    try { worker.postMessage({ type: "RETROM_ACTIVATE_UPDATE" }, [channel.port2]); }
    catch { finish(new Error("更新暂不可用，请稍后重试。")); }
  });
}
