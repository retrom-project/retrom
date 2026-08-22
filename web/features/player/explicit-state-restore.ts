import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";

const emulatorJsVersionURL = "https://cdn.emulatorjs.org/stable/data/version.json";
const stateFilePath = "/game.state";

type RuntimeModuleConfig = {
  print?: (...args: unknown[]) => void;
  printErr?: (...args: unknown[]) => void;
  [name: string]: unknown;
};

type RuntimeFactory = ((config: RuntimeModuleConfig) => unknown) & { retromStateRestoreHook?: boolean };

type StateRestoreManager = NonNullable<EmulatorInstance["gameManager"]> & {
  clearEJSResetTimer?: () => void;
  loadExplicitStateAndWait?: (state: Uint8Array, timeoutMs?: number) => Promise<void>;
};

type GameManagerConstructor = { prototype?: StateRestoreManager };

type StateRestoreWindow = Window & {
  EJS_Runtime?: RuntimeFactory;
  EJS_GameManager?: GameManagerConstructor;
};

type LoadSignal = {
  loading: boolean;
  reject: (error: Error) => void;
  resolve: () => void;
  timer: number;
};

type ReadyWait = {
  reject: (error: Error) => void;
  timer: number;
};

export function requiresExplicitStateRestore(config: {
  emulatorjsVersion: string;
  runtimeCore?: string;
  stateUrl: string | null;
}) {
  return config.stateUrl !== null && (config.emulatorjsVersion === "4.2.3" ||
    config.emulatorjsVersion === "4.3.0-pre" && config.runtimeCore === "dosbox_pure");
}

function removeItem<T>(items: T[], item: T) {
  const index = items.indexOf(item);
  if (index >= 0) items.splice(index, 1);
  return index >= 0;
}

/**
 * EmulatorJS 4.2.3 may ask a core to restore before it has established its
 * serialization layout. Retrom therefore restores every explicitly selected
 * state only after a completed frame and successful serialization probe, then
 * waits for the native RetroArch state task and fails closed if it rejects the
 * file. This covers PPSSPP GPU setup, FBA state-size initialization, MAME's
 * frame-zero restriction, and the same race in any other 4.2.3 core.
 */
export function installEmulatorJs423StateRestoreCompatibility(
  playerWindow: Window = window,
  options: { waitForSerializable?: boolean } = {},
) {
  const target = playerWindow as StateRestoreWindow;
  const waitForSerializable = options.waitForSerializable ?? false;
  const loadSignals: LoadSignal[] = [];
  const readyWaits = new Set<ReadyWait>();
  const prototypeRestores: Array<() => void> = [];
  let active = true;

  const delay = (delayMs: number) => new Promise<void>((resolve, reject) => {
    const wait: ReadyWait = {
      reject,
      timer: target.setTimeout(() => {
        readyWaits.delete(wait);
        resolve();
      }, delayMs),
    };
    readyWaits.add(wait);
  });

  const registerLoadSignal = (timeoutMs: number) => {
    let signal!: LoadSignal;
    const promise = new Promise<void>((resolve, reject) => {
      signal = {
        loading: false,
        reject,
        resolve,
        timer: target.setTimeout(() => {
          if (!removeItem(loadSignals, signal)) return;
          reject(new Error("PLAYER_SAVE_STATE_RESTORE_TIMEOUT"));
        }, timeoutMs),
      };
      loadSignals.push(signal);
    });
    const finish = (error?: Error) => {
      if (!removeItem(loadSignals, signal)) return;
      target.clearTimeout(signal.timer);
      if (error) signal.reject(error);
      else signal.resolve();
    };
    return { promise, cancel: () => finish(), fail: (error: Error) => finish(error) };
  };

  const observeNativeLog = (args: unknown[]) => {
    const message = args.map(String).join(" ");
    if (!message.includes("[State]") || !message.includes("game.state")) return;
    const signal = loadSignals[0];
    if (!signal) return;
    if (/failed/i.test(message)) {
      signal.loading = false;
      target.queueMicrotask(() => {
        if (loadSignals[0] === signal) {
          target.clearTimeout(signal.timer);
          loadSignals.shift();
          signal.reject(new Error("PLAYER_SAVE_STATE_RESTORE_FAILED"));
        }
      });
      return;
    }
    if (!/loading state/i.test(message)) return;
    signal.loading = true;
    target.queueMicrotask(() => {
      if (loadSignals[0] !== signal || !signal.loading) return;
      target.clearTimeout(signal.timer);
      loadSignals.shift();
      signal.resolve();
    });
  };

  const patchManager = (constructor: GameManagerConstructor | undefined) => {
    const prototype = constructor?.prototype;
    if (!prototype || prototype.loadExplicitStateAndWait) {
      throw new Error("PLAYER_STATE_RESTORE_COMPATIBILITY_UNAVAILABLE");
    }
    const loadExplicitStateAndWait = async function (this: StateRestoreManager, state: Uint8Array, timeoutMs = 15_000) {
      const fileSystem = this.FS;
      const nativeLoadState = this.functions?.loadState;
      const nativeSaveStateInfo = this.functions?.saveStateInfo;
      if (!this.toggleMainLoop || !fileSystem?.writeFile || !fileSystem.unlink ||
        typeof nativeLoadState !== "function" ||
        !(state instanceof Uint8Array) || state.byteLength === 0) {
        throw new Error("PLAYER_STATE_RESTORE_COMPATIBILITY_UNAVAILABLE");
      }
      if (waitForSerializable && typeof nativeSaveStateInfo !== "function") {
        throw new Error("PLAYER_STATE_RESTORE_COMPATIBILITY_UNAVAILABLE");
      }

      const deadline = target.performance.now() + timeoutMs;
      if (waitForSerializable && nativeSaveStateInfo) {
        while (active) {
          try {
            const [size, , succeeded] = String(nativeSaveStateInfo.call(this.functions)).split("|");
            const frameReady = typeof this.getFrameNum !== "function" || this.getFrameNum() > 0;
            if (frameReady && succeeded === "1" && Number.isSafeInteger(Number(size)) && Number(size) > 0) break;
          } catch {
            // PPSSPP returns an invalid state before its GPU has been created.
          }
          if (target.performance.now() >= deadline) throw new Error("PLAYER_SAVE_STATE_RESTORE_TIMEOUT");
          this.toggleMainLoop(true);
          await delay(Math.min(50, Math.max(1, deadline - target.performance.now())));
        }
      }
      if (!active) throw new Error("PLAYER_SESSION_ENDED");

      const remainingMs = Math.max(1, deadline - target.performance.now());
      const completion = registerLoadSignal(remainingMs);
      try {
        try { fileSystem.unlink(stateFilePath); } catch { /* absent before the first load */ }
        fileSystem.writeFile(stateFilePath, new Uint8Array(state));
        this.clearEJSResetTimer?.();
        nativeLoadState.call(this.functions, "game.state", 0);
        this.toggleMainLoop(true);
        await completion.promise;
      } finally {
        completion.cancel();
        this.toggleMainLoop(false);
        try { fileSystem.unlink(stateFilePath); } catch { /* native code may already remove it */ }
      }
    };
    prototype.loadExplicitStateAndWait = loadExplicitStateAndWait;
    prototypeRestores.push(() => Reflect.deleteProperty(prototype, "loadExplicitStateAndWait"));
  };

  const managerDescriptor = Object.getOwnPropertyDescriptor(target, "EJS_GameManager");
  if (managerDescriptor && !managerDescriptor.configurable) throw new Error("PLAYER_STATE_RESTORE_COMPATIBILITY_UNAVAILABLE");
  let managerConstructor = target.EJS_GameManager;
  if (managerConstructor) patchManager(managerConstructor);
  Object.defineProperty(target, "EJS_GameManager", {
    configurable: true,
    enumerable: managerDescriptor?.enumerable ?? true,
    get: () => managerConstructor,
    set: (constructor: GameManagerConstructor | undefined) => {
      patchManager(constructor);
      managerConstructor = constructor;
    },
  });

  const wrapRuntime = (factory: RuntimeFactory | undefined) => {
    if (typeof factory !== "function") throw new Error("PLAYER_STATE_RESTORE_COMPATIBILITY_UNAVAILABLE");
    if (factory.retromStateRestoreHook) return factory;
    const wrapped = function (this: unknown, moduleConfig: RuntimeModuleConfig) {
      return Reflect.apply(factory, this, [{
        ...moduleConfig,
        print: (...args: unknown[]) => { moduleConfig?.print?.(...args); observeNativeLog(args); },
        printErr: (...args: unknown[]) => { moduleConfig?.printErr?.(...args); observeNativeLog(args); },
      }]);
    } as RuntimeFactory;
    Object.defineProperty(wrapped, "retromStateRestoreHook", { value: true });
    return wrapped;
  };

  const runtimeDescriptor = Object.getOwnPropertyDescriptor(target, "EJS_Runtime");
  if (runtimeDescriptor && !runtimeDescriptor.configurable) throw new Error("PLAYER_STATE_RESTORE_COMPATIBILITY_UNAVAILABLE");
  let runtimeFactory = target.EJS_Runtime ? wrapRuntime(target.EJS_Runtime) : undefined;
  Object.defineProperty(target, "EJS_Runtime", {
    configurable: true,
    enumerable: runtimeDescriptor?.enumerable ?? true,
    get: () => runtimeFactory,
    set: (factory: RuntimeFactory | undefined) => { runtimeFactory = wrapRuntime(factory); },
  });

  const originalFetch = target.fetch.bind(target);
  target.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" || input instanceof URL ? String(input) : input.url;
    if (url === emulatorJsVersionURL) {
      return Promise.resolve(new Response(JSON.stringify({ version: "4.2.3", current_version: "4.2.3" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }));
    }
    return originalFetch(input, init);
  }) as typeof fetch;

  return () => {
    if (!active) return;
    active = false;
    target.fetch = originalFetch;
    for (const restore of prototypeRestores.reverse()) restore();
    if (managerDescriptor) Object.defineProperty(target, "EJS_GameManager", managerDescriptor);
    else Reflect.deleteProperty(target, "EJS_GameManager");
    if (runtimeDescriptor) Object.defineProperty(target, "EJS_Runtime", runtimeDescriptor);
    else Reflect.deleteProperty(target, "EJS_Runtime");
    for (const wait of readyWaits) {
      target.clearTimeout(wait.timer);
      wait.reject(new Error("PLAYER_SESSION_ENDED"));
    }
    readyWaits.clear();
    for (const signal of loadSignals.splice(0)) {
      target.clearTimeout(signal.timer);
      signal.reject(new Error("PLAYER_SESSION_ENDED"));
    }
  };
}
