const VERSION_URL = "https://cdn.emulatorjs.org/stable/data/version.json";

function equalBytes(left, right) {
  if (left.byteLength !== right.byteLength) return false;
  for (let index = 0; index < left.byteLength; index += 1) {
    if (left[index] !== right[index]) return false;
  }
  return true;
}

export function installEmulatorJs423NetplayPatch(target = window) {
  const loadSignals = [];
  const registerLoadSignal = () => {
    let rejectSignal;
    let resolveSignal;
    const signal = { reject: null, resolve: null };
    const promise = new Promise((resolve, reject) => {
      rejectSignal = reject;
      resolveSignal = resolve;
    });
    signal.reject = rejectSignal;
    signal.resolve = resolveSignal;
    loadSignals.push(signal);
    return {
      promise,
      cancel() {
        const index = loadSignals.indexOf(signal);
        if (index !== -1) loadSignals.splice(index, 1);
      }
    };
  };
  const observeNativeLog = (args) => {
    const message = args.map(String).join(" ");
    if (!message.includes("[State]") || !message.includes("game.state")) return;
    const signal = loadSignals.shift();
    if (!signal) return;
    if (/failed/i.test(message)) {
      target.queueMicrotask(() => signal.reject(new Error("RetroArch rejected the savestate")));
    } else if (/loading state/i.test(message)) {
      // The log call is synchronous inside content_load_state_cb. A microtask
      // runs only after that native callback and its deserialize work return.
      target.queueMicrotask(() => signal.resolve());
    } else {
      loadSignals.unshift(signal);
    }
  };
  const report = {
    version: "4.2.3",
    managerPatched: false,
    inMemorySaves: false,
    offlineVersionCheck: false,
    frameHookBootstrap: false,
    waitableStateLoad: false,
    exactFrameStep: false,
    provenStateLoads: 0,
    exactFrameSteps: 0,
    wakeLockFallback: false,
    errors: []
  };

  const wakeLock = target.navigator?.wakeLock;
  if (wakeLock && typeof wakeLock.request === "function") {
    const originalRequest = wakeLock.request.bind(wakeLock);
    try {
      wakeLock.request = async (...args) => {
        try {
          return await originalRequest(...args);
        } catch {
          report.wakeLockFallback = true;
          return { released: false, release: async () => {} };
        }
      };
    } catch {
      // Some browsers expose a non-writable WakeLock object. Netplay remains
      // functional; the capability report keeps this fallback false.
    }
  }

  const originalFetch = target.fetch.bind(target);
  target.fetch = (input, init) => {
    const url = typeof input === "string" ? input : input?.url;
    if (url === VERSION_URL) {
      report.offlineVersionCheck = true;
      return Promise.resolve(new Response(JSON.stringify({ version: "4.2.3", current_version: "4.2.3" }), {
        status: 200,
        headers: { "Content-Type": "application/json" }
      }));
    }
    return originalFetch(input, init);
  };

  let gameManagerConstructor = target.EJS_GameManager;
  const patchConstructor = (constructor) => {
    const prototype = constructor?.prototype;
    if (!prototype || typeof prototype.mountFileSystems !== "function") {
      throw new Error("EmulatorJS 4.2.3 GameManager mountFileSystems is unavailable");
    }
    if (prototype.mountFileSystems.retromNetplayInMemory === true) return;
    const mountInMemory = function mountInMemory() {
      this.mkdir("/data");
      this.mkdir("/data/saves");
      return Promise.resolve();
    };
    Object.defineProperty(mountInMemory, "retromNetplayInMemory", { value: true });
    prototype.mountFileSystems = mountInMemory;

    if (typeof prototype.loadStateAndWait !== "function") {
      prototype.loadStateAndWait = async function loadStateAndWait(state, timeoutMs = 5000) {
        const expected = new Uint8Array(state);
        const before = this.getState();
        const changed = !equalBytes(before, expected);
        if (!changed) return { changed: false, nativeCompletion: false, stateBytes: expected.byteLength };
        const completion = registerLoadSignal();
        this.loadState(expected);
        try {
          // RetroArch's blocking state task does not progress while the
          // Emscripten loop is paused. Resume it under the bridge's suppressed
          // output boundary, then pause again as soon as the native callback
          // has returned. The loaded state is therefore observed before the
          // next core frame.
          this.toggleMainLoop(1);
          let timeout;
          try {
            await Promise.race([
              completion.promise,
              new Promise((_, reject) => {
                timeout = target.setTimeout(
                  () => reject(new Error("EmulatorJS savestate task did not report native completion")),
                  timeoutMs
                );
              })
            ]);
          } finally {
            target.clearTimeout(timeout);
          }
          this.toggleMainLoop(0);
          const byteExact = equalBytes(this.getState(), expected);
          report.provenStateLoads += 1;
          return {
            byteExact,
            changed: true,
            nativeCompletion: true,
            stateBytes: expected.byteLength
          };
        } finally {
          this.toggleMainLoop(0);
          completion.cancel();
        }
      };
    }

    if (typeof prototype.runNetplayFrame !== "function") {
      prototype.runNetplayFrame = function runNetplayFrame(timeoutMs = 1000) {
        return new Promise((resolve, reject) => {
          const original = target.__RETROM_POST_MAIN_LOOP__;
          const startFrame = this.getFrameNum();
          const timer = target.setTimeout(() => {
            if (target.__RETROM_POST_MAIN_LOOP__ === wrapper) {
              target.__RETROM_POST_MAIN_LOOP__ = original;
            }
            reject(new Error("EmulatorJS exact frame step timed out"));
          }, timeoutMs);
          const wrapper = () => {
            if (typeof original === "function") original();
            const completedFrame = this.getFrameNum();
            if (completedFrame <= startFrame) return;
            target.clearTimeout(timer);
            this.toggleMainLoop(0);
            if (target.__RETROM_POST_MAIN_LOOP__ === wrapper) {
              target.__RETROM_POST_MAIN_LOOP__ = original;
            }
            report.exactFrameSteps += 1;
            resolve(completedFrame);
          };
          target.__RETROM_POST_MAIN_LOOP__ = wrapper;
          this.toggleMainLoop(1);
        });
      };
    }
    report.managerPatched = true;
    report.inMemorySaves = true;
    report.waitableStateLoad = true;
    report.exactFrameStep = true;
  };

  if (gameManagerConstructor) patchConstructor(gameManagerConstructor);
  Object.defineProperty(target, "EJS_GameManager", {
    configurable: true,
    enumerable: true,
    get: () => gameManagerConstructor,
    set: (constructor) => {
      try {
        patchConstructor(constructor);
        gameManagerConstructor = constructor;
      } catch (error) {
        report.errors.push(error instanceof Error ? error.message : String(error));
        throw error;
      }
    }
  });

  let runtimeFactory = target.EJS_Runtime;
  const wrapRuntimeFactory = (factory) => {
    if (typeof factory !== "function") throw new Error("EmulatorJS EJS_Runtime factory is unavailable");
    if (factory.retromNetplayFrameHook === true) return factory;
    const wrapped = function retromNetplayRuntimeFactory(moduleConfig) {
      const originalPostMainLoop = moduleConfig?.postMainLoop;
      const patchedConfig = {
        ...moduleConfig,
        print(...args) {
          if (typeof moduleConfig?.print === "function") moduleConfig.print(...args);
          observeNativeLog(args);
        },
        printErr(...args) {
          if (typeof moduleConfig?.printErr === "function") moduleConfig.printErr(...args);
          observeNativeLog(args);
        },
        postMainLoop() {
          if (typeof originalPostMainLoop === "function") originalPostMainLoop();
          if (typeof target.__RETROM_POST_MAIN_LOOP__ === "function") {
            target.__RETROM_POST_MAIN_LOOP__();
          }
        }
      };
      report.frameHookBootstrap = true;
      const originalLog = target.console.log;
      target.console.log = function observeRetroArchStateLog(...args) {
        originalLog.apply(this, args);
        observeNativeLog(args);
      };
      try {
        return Reflect.apply(factory, this, [patchedConfig]);
      } finally {
        target.console.log = originalLog;
      }
    };
    Object.defineProperty(wrapped, "retromNetplayFrameHook", { value: true });
    return wrapped;
  };
  if (runtimeFactory) runtimeFactory = wrapRuntimeFactory(runtimeFactory);
  Object.defineProperty(target, "EJS_Runtime", {
    configurable: true,
    enumerable: true,
    get: () => runtimeFactory,
    set: (factory) => {
      runtimeFactory = wrapRuntimeFactory(factory);
    }
  });

  target.__RETROM_EJS_423_PATCH__ = report;
  return report;
}
