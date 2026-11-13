const retryDelay = 5_000;
const probeTimeout = 3_000;

/** Browser connectivity can stay stale after a PWA resumes or its game fails. */
export function observeConnection(changed: (online: boolean) => void) {
  let online = true;
  let disposed = false;
  let request: AbortController | undefined;
  let retry: ReturnType<typeof setTimeout> | undefined;
  let deadline: ReturnType<typeof setTimeout> | undefined;
  const publish = (next: boolean) => {
    if (online !== next) { online = next; changed(next); }
  };
  function cancel() {
    clearTimeout(retry);
    clearTimeout(deadline);
    const previous = request;
    request = undefined;
    previous?.abort();
  }
  async function probe() {
    if (disposed || request || document.visibilityState !== "visible") { return; }
    if (navigator.onLine) { publish(true); return; }
    const controller = new AbortController();
    request = controller;
    deadline = setTimeout(() => controller.abort(), probeTimeout);
    try {
      // Any HTTP response proves connectivity. Readiness errors belong to ServiceHealth.
      await fetch("/health/ready", { cache: "no-store", signal: controller.signal });
      if (request === controller) { publish(true); }
    } catch {
      if (request === controller) { publish(false); }
    } finally {
      if (request === controller) {
        request = undefined;
        clearTimeout(deadline);
        if (!online) { retry = setTimeout(() => { void probe(); }, retryDelay); }
      }
    }
  }
  function connection() {
    cancel();
    publish(navigator.onLine);
    if (!online) { void probe(); }
  }
  function resume() {
    if (document.visibilityState !== "visible") { cancel(); return; }
    clearTimeout(retry);
    if (navigator.onLine) { cancel(); publish(true); } else { void probe(); }
  }
  const initial = setTimeout(connection, 0);
  window.addEventListener("online", connection);
  window.addEventListener("offline", connection);
  window.addEventListener("focus", resume);
  window.addEventListener("pageshow", resume);
  document.addEventListener("visibilitychange", resume);
  return () => {
    disposed = true;
    clearTimeout(initial);
    cancel();
    window.removeEventListener("online", connection);
    window.removeEventListener("offline", connection);
    window.removeEventListener("focus", resume);
    window.removeEventListener("pageshow", resume);
    document.removeEventListener("visibilitychange", resume);
  };
}
