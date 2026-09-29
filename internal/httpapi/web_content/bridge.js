(() => {
  "use strict";
  const script = document.currentScript;
  const parentOrigin = script.dataset.parent;
  const entry = script.dataset.entry;
  const workerURL = script.dataset.worker;
  function connect(port) {
    parent.postMessage({type: "RETROM_WEB_CONTENT_CONNECT", v: 1}, parentOrigin, [port]);
  }
  navigator.serviceWorker?.addEventListener("message", event => {
    if (event.source !== navigator.serviceWorker.controller || event.data?.type !== "RETROM_WEB_CONTENT_RECONNECT" || event.ports.length !== 1) return;
    connect(event.ports[0]);
  });
  function receive(port, expected) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {port.close(); reject(new Error("CONTENT_IO_TIMEOUT"));}, 10000);
      port.onmessage = ({data}) => {
        if (data?.type !== expected) return;
        clearTimeout(timer); port.onmessage = null; resolve(data);
      };
      port.start();
    });
  }
  async function start() {
    const content = new MessageChannel();
    const ready = receive(content.port1, "READY");
    connect(content.port2);
    const mode = await ready;
    if (!navigator.serviceWorker || !isSecureContext) {
      content.port1.close();
      if (mode.preloaded) throw new Error("CONTENT_IO_STORAGE_UNAVAILABLE");
      location.replace(entry); return;
    }
    try {await startWorker(content.port1);}
    catch (error) {
      content.port1.close();
      if (mode.preloaded) throw error;
      location.replace(entry);
    }
  }
  async function startWorker(contentPort) {
    const registration = await navigator.serviceWorker.register(workerURL, {scope: "/__retrom/", updateViaCache: "none"});
    await navigator.serviceWorker.ready;
    const worker = registration.active;
    if (!worker) throw new Error("CONTENT_IO_WORKER_UNAVAILABLE");
    const reply = new MessageChannel();
    const connected = receive(reply.port1, "CONNECTED");
    worker.postMessage({type: "CONNECT"}, [contentPort, reply.port2]);
    try {await connected;} finally {reply.port1.close();}
    location.replace(entry);
  }
  function failed(error) {
    document.body.textContent = error.message;
    parent.postMessage({type: "RETROM_WEB_CONTENT_FAILED", v: 1, code: error.message}, parentOrigin);
  }
  if (entry && workerURL) {
    let started = false;
    addEventListener("message", event => {
      if (started || event.source !== parent || event.origin !== parentOrigin ||
          event.data?.type !== "RETROM_WEB_CONTENT_START" || event.data.v !== 1) return;
      started = true; void start().catch(failed);
    });
    parent.postMessage({type: "RETROM_WEB_CONTENT_WAITING", v: 1}, parentOrigin);
  }
})();
