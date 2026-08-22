import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";

const stateFilePath = "/game.state";
const dosboxPureStackMarker = Uint8Array.of(
  0x23, 0x0c, 0x45, 0x04, 0x40, 0x20, 0x01, 0x41, 0xc0, 0x02,
  0x6a, 0x24, 0x00, 0x20, 0x01, 0x41, 0x10, 0x6a, 0x0f,
);
// The pinned 4.3.0-pre thread artifact was linked with a 4 MiB stack. DOSBox
// Pure serializes through that stack and corrupts save_state_info before it can
// describe a state. Keep the same stack base and extend its two linked high
// watermarks to 64 MiB. Both values are equal-length unsigned LEB128 encodings.
const linkedStackHigh = Uint8Array.of(0xf0, 0xec, 0x80, 0x0e);
const compatibleStackHigh = Uint8Array.of(0xf0, 0xec, 0x80, 0x2c);

type DOSBoxModule = {
  HEAPU8?: Uint8Array;
  UTF8ToString?: (pointer: number) => string;
  _free?: (pointer: number) => void;
  _save_state_info?: () => number;
};

type DOSBoxManager = NonNullable<EmulatorInstance["gameManager"]> & {
  Module?: DOSBoxModule;
  clearEJSResetTimer?: () => void;
};

type GameManagerConstructor = { prototype?: DOSBoxManager };
type RuntimeModuleConfig = {
  postMainLoop?: (...args: unknown[]) => void;
  print?: (...args: unknown[]) => void;
  printErr?: (...args: unknown[]) => void;
  [name: string]: unknown;
};
type RuntimeFactory = ((config: RuntimeModuleConfig) => unknown) & { retromDOSBoxStateHook?: boolean };
type DOSBoxWindow = Window & {
  EJS_GameManager?: GameManagerConstructor;
  EJS_Runtime?: RuntimeFactory;
  WebAssembly: typeof WebAssembly;
};

type LoadSignal = {
  loading: boolean;
  reject: (error: Error) => void;
  resolve: () => void;
  timer: number;
};

type WaitSignal = {
  reject: (error: Error) => void;
  timer: number;
};

type MainLoopSignal = {
  reject: (error: Error) => void;
  resolve: () => void;
  timer: number;
};

type OperationSignal = { promise: Promise<void>; cancel: () => void };
type DOSLoadDependencies = {
  target: DOSBoxWindow;
  isActive: () => boolean;
  delay: (delayMs: number) => Promise<void>;
  registerLoadSignal: (timeoutMs: number) => OperationSignal;
  registerMainLoopSignal: (timeoutMs: number) => OperationSignal;
};

function matchingOffsets(bytes: Uint8Array, pattern: Uint8Array) {
  const offsets: number[] = [];
  for (let at = 0; at <= bytes.byteLength - pattern.byteLength; at += 1) {
    let matches = true;
    for (let offset = 0; offset < pattern.byteLength; offset += 1) {
      if (bytes[at + offset] !== pattern[offset]) {
        matches = false;
        break;
      }
    }
    if (matches) {offsets.push(at);}
  }
  return offsets;
}

function equalBytes(left: Uint8Array, right: Uint8Array) {
  if (left.byteLength !== right.byteLength) {return false;}
  for (let index = 0; index < left.byteLength; index += 1) {
    if (left[index] !== right[index]) {return false;}
  }
  return true;
}

function coreStatePayload(state: Uint8Array) {
  const signature = Uint8Array.of(0x52, 0x41, 0x53, 0x54, 0x41, 0x54, 0x45, 0x01);
  if (state.byteLength < signature.byteLength || !equalBytes(state.subarray(0, 8), signature)) {
    throw new Error("PLAYER_STATE_UNAVAILABLE");
  }
  for (let offset = 8; offset + 8 <= state.byteLength;) {
    const marker = String.fromCharCode(...state.subarray(offset, offset + 4));
    const size = state[offset + 4]! | state[offset + 5]! << 8 |
      state[offset + 6]! << 16 | state[offset + 7]! << 24;
    if (size < 0 || offset + 8 + size > state.byteLength) {throw new Error("PLAYER_STATE_UNAVAILABLE");}
    if (marker === "MEM ") {return state.subarray(offset + 8, offset + 8 + size);}
    if (marker === "END ") {break;}
    offset += 8 + (size + 7 & ~7);
  }
  throw new Error("PLAYER_STATE_UNAVAILABLE");
}

/** Returns null for unrelated WASM and fails closed for a changed DOSBox build. */
export function patchDOSBoxPureStateStack(source: BufferSource) {
  const view = source instanceof ArrayBuffer
    ? new Uint8Array(source)
    : new Uint8Array(source.buffer, source.byteOffset, source.byteLength);
  const markers = matchingOffsets(view, dosboxPureStackMarker);
  if (markers.length === 0) {return null;}
  const stackHighOffsets = matchingOffsets(view, linkedStackHigh);
  if (markers.length !== 1 || stackHighOffsets.length !== 2) {
    throw new Error("PLAYER_DOS_STATE_COMPATIBILITY_UNAVAILABLE");
  }
  const patched = view.slice();
  for (const offset of stackHighOffsets) {patched.set(compatibleStackHigh, offset);}
  if (!WebAssembly.validate(patched)) {throw new Error("PLAYER_DOS_STATE_COMPATIBILITY_UNAVAILABLE");}
  return patched;
}

/**
 * The pinned EmulatorJS helper frees save_state_info even though the exported C
 * function returns a stack buffer. Read it synchronously, copy the allocated
 * state, and free only the state allocation.
 */
export function readDOSBoxPureState(module: DOSBoxModule) {
  const heap = module.HEAPU8;
  if (!heap || typeof module.UTF8ToString !== "function" || typeof module._free !== "function" ||
    typeof module._save_state_info !== "function") {
    throw new Error("PLAYER_DOS_STATE_COMPATIBILITY_UNAVAILABLE");
  }
  const info = module._save_state_info();
  if (!Number.isSafeInteger(info) || info <= 0 || info >= heap.byteLength) {
    throw new Error("PLAYER_STATE_UNAVAILABLE");
  }
  const [rawSize, rawStart, succeeded] = module.UTF8ToString(info).split("|");
  const size = Number.parseInt(rawSize, 10);
  const dataStart = Number.parseInt(rawStart, 10);
  if (succeeded !== "1" || !Number.isSafeInteger(size) || size <= 0 ||
    !Number.isSafeInteger(dataStart) || dataStart < 0 || dataStart + size > heap.byteLength) {
    throw new Error("PLAYER_STATE_UNAVAILABLE");
  }
  const state = heap.slice(dataStart, dataStart + size);
  module._free(dataStart);
  return state;
}

export function requiresDOSBoxPureStateCompatibility(config: {
  emulatorjsVersion: string;
  runtimeCore: string;
}) {
  return config.emulatorjsVersion === "4.3.0-pre" && config.runtimeCore === "dosbox_pure";
}

export function canCreateRecoverableManualState(config: {
  runtimeCore: string;
  dosEntry?: string | null;
}) {
  return config.runtimeCore !== "dosbox_pure" || Boolean(config.dosEntry);
}

async function waitForDOSBoxSerialization(manager: DOSBoxManager, toggleMainLoop: (running: boolean) => void, deadline: number, dependencies: DOSLoadDependencies) {
  while (dependencies.isActive()) {
    try {
      if (manager.getState?.().byteLength) {return;}
    } catch {
      // DOSBox Pure rejects serialization until the selected program runs.
    }
    if (dependencies.target.performance.now() >= deadline) {throw new Error("PLAYER_SAVE_STATE_RESTORE_TIMEOUT");}
    toggleMainLoop(true);
    await dependencies.delay(Math.min(50, Math.max(1, deadline - dependencies.target.performance.now())));
  }
  throw new Error("PLAYER_SESSION_ENDED");
}

function dosBoxLoadResources(manager: DOSBoxManager, state: Uint8Array) {
  const fileSystem = manager.FS;
  const nativeLoadState = manager.functions?.loadState;
  if (!manager.toggleMainLoop || !fileSystem?.writeFile || !fileSystem.unlink || typeof nativeLoadState !== "function" || !(state instanceof Uint8Array) || state.byteLength === 0) {
    throw new Error("PLAYER_DOS_STATE_COMPATIBILITY_UNAVAILABLE");
  }
  return { fileSystem, nativeLoadState, toggleMainLoop: manager.toggleMainLoop.bind(manager) };
}

function createDOSBoxStateLoader(dependencies: DOSLoadDependencies) {
  return async function loadExplicitStateAndWait(this: DOSBoxManager, state: Uint8Array, timeoutMs = 30_000) {
    const { fileSystem, nativeLoadState, toggleMainLoop } = dosBoxLoadResources(this, state);
    coreStatePayload(state);
    const deadline = dependencies.target.performance.now() + timeoutMs;
    await waitForDOSBoxSerialization(this, toggleMainLoop, deadline, dependencies);
    toggleMainLoop(false);
    await dependencies.delay(50);
    if (!dependencies.isActive()) {throw new Error("PLAYER_SESSION_ENDED");}
    try {
      try {fileSystem.unlink(stateFilePath);} catch { /* absent before the first load */ }
      fileSystem.writeFile(stateFilePath, new Uint8Array(state));
      const remaining = () => Math.max(1, deadline - dependencies.target.performance.now());
      const completion = dependencies.registerLoadSignal(remaining());
      const loadIteration = dependencies.registerMainLoopSignal(remaining());
      try {
        this.clearEJSResetTimer?.();
        nativeLoadState.call(this.functions, stateFilePath, 0);
        toggleMainLoop(true);
        await Promise.all([completion.promise, loadIteration.promise]);
        toggleMainLoop(false);
        const observed = this.getState?.();
        if (!observed?.byteLength) {throw new Error("PLAYER_SAVE_STATE_RESTORE_FAILED");}
        coreStatePayload(observed);
      } finally {
        loadIteration.cancel(); completion.cancel(); toggleMainLoop(false);
      }
    } finally {
      toggleMainLoop(false);
      try {fileSystem.unlink(stateFilePath);} catch { /* native code may already remove it */ }
    }
  };
}

export function installDOSBoxPureStateCompatibility(playerWindow: Window = window) {
  const target = playerWindow as DOSBoxWindow;
  const originalInstantiate = target.WebAssembly.instantiate.bind(target.WebAssembly);
  const originalInstantiateStreaming = target.WebAssembly.instantiateStreaming?.bind(target.WebAssembly);
  const managerDescriptor = Object.getOwnPropertyDescriptor(target, "EJS_GameManager");
  const runtimeDescriptor = Object.getOwnPropertyDescriptor(target, "EJS_Runtime");
  if (managerDescriptor && !managerDescriptor.configurable || runtimeDescriptor && !runtimeDescriptor.configurable) {
    throw new Error("PLAYER_DOS_STATE_COMPATIBILITY_UNAVAILABLE");
  }

  let active = true;
  let patchedArtifact = false;
  let managerConstructor = target.EJS_GameManager;
  let runtimeFactory = target.EJS_Runtime;
  const loadSignals: LoadSignal[] = [];
  const mainLoopSignals: MainLoopSignal[] = [];
  const waitSignals = new Set<WaitSignal>();
  const patchedPrototypes = new Set<DOSBoxManager>();
  const prototypeDescriptors = new Map<DOSBoxManager, {
    getState?: PropertyDescriptor;
    loadExplicitStateAndWait?: PropertyDescriptor;
  }>();

  const delay = (delayMs: number) => new Promise<void>((resolve, reject) => {
    const signal: WaitSignal = {
      reject,
      timer: target.setTimeout(() => {
        waitSignals.delete(signal);
        resolve();
      }, delayMs),
    };
    waitSignals.add(signal);
    if (!active) {
      target.clearTimeout(signal.timer);
      waitSignals.delete(signal);
      reject(new Error("PLAYER_SESSION_ENDED"));
    }
  });

  const finishSignal = (signal: LoadSignal, error?: Error) => {
    const index = loadSignals.indexOf(signal);
    if (index < 0) {return;}
    loadSignals.splice(index, 1);
    target.clearTimeout(signal.timer);
    if (error) {signal.reject(error);}
    else {signal.resolve();}
  };

  const registerLoadSignal = (timeoutMs: number) => {
    let signal!: LoadSignal;
    const promise = new Promise<void>((resolve, reject) => {
      signal = {
        loading: false,
        reject,
        resolve,
        timer: target.setTimeout(() => finishSignal(signal, new Error("PLAYER_SAVE_STATE_RESTORE_TIMEOUT")), timeoutMs),
      };
      loadSignals.push(signal);
    });
    return { promise, cancel: () => finishSignal(signal) };
  };

  const finishMainLoopSignal = (signal: MainLoopSignal, error?: Error) => {
    const index = mainLoopSignals.indexOf(signal);
    if (index < 0) {return;}
    mainLoopSignals.splice(index, 1);
    target.clearTimeout(signal.timer);
    if (error) {signal.reject(error);}
    else {signal.resolve();}
  };

  const registerMainLoopSignal = (timeoutMs: number) => {
    let signal!: MainLoopSignal;
    const promise = new Promise<void>((resolve, reject) => {
      signal = {
        reject,
        resolve,
        timer: target.setTimeout(() => finishMainLoopSignal(
          signal, new Error("PLAYER_SAVE_STATE_RESTORE_TIMEOUT"),
        ), timeoutMs),
      };
      mainLoopSignals.push(signal);
    });
    return { promise, cancel: () => finishMainLoopSignal(signal) };
  };

  const observeNativeLog = (args: unknown[]) => {
    const message = args.map(String).join(" ");
    if (!message.includes("[State]") || !message.includes("game.state")) {return;}
    const signal = loadSignals[0];
    if (!signal) {return;}
    if (/failed/i.test(message)) {
      signal.loading = false;
      target.queueMicrotask(() => {
        if (loadSignals[0] === signal) {finishSignal(signal, new Error("PLAYER_SAVE_STATE_RESTORE_FAILED"));}
      });
      return;
    }
    if (/loading state/i.test(message)) {
      signal.loading = true;
      target.queueMicrotask(() => {
        if (loadSignals[0] === signal && signal.loading) {finishSignal(signal);}
      });
    }
  };

  const patchManager = (constructor: GameManagerConstructor | undefined) => {
    const prototype = constructor?.prototype;
    if (!prototype || patchedPrototypes.has(prototype)) {return;}
    if (!patchedArtifact) {return;}
    if (prototype.loadExplicitStateAndWait) {
      throw new Error("PLAYER_DOS_STATE_COMPATIBILITY_UNAVAILABLE");
    }
    prototypeDescriptors.set(prototype, {
      getState: Object.getOwnPropertyDescriptor(prototype, "getState"),
      loadExplicitStateAndWait: Object.getOwnPropertyDescriptor(prototype, "loadExplicitStateAndWait"),
    });
    prototype.getState = function (this: DOSBoxManager) {
      return readDOSBoxPureState(this.Module ?? {});
    };
    prototype.loadExplicitStateAndWait = createDOSBoxStateLoader({ target, isActive: () => active, delay, registerLoadSignal, registerMainLoopSignal });
    patchedPrototypes.add(prototype);
  };

  const wrapRuntime = (factory: RuntimeFactory | undefined) => {
    if (typeof factory !== "function") {throw new Error("PLAYER_DOS_STATE_COMPATIBILITY_UNAVAILABLE");}
    if (factory.retromDOSBoxStateHook) {return factory;}
    const wrapped = function (this: unknown, moduleConfig: RuntimeModuleConfig) {
      return Reflect.apply(factory, this, [{
        ...moduleConfig,
        postMainLoop: (...args: unknown[]) => {
          moduleConfig?.postMainLoop?.(...args);
          const signal = mainLoopSignals[0];
          if (signal) {finishMainLoopSignal(signal);}
        },
        print: (...args: unknown[]) => { moduleConfig?.print?.(...args); observeNativeLog(args); },
        printErr: (...args: unknown[]) => { moduleConfig?.printErr?.(...args); observeNativeLog(args); },
      }]);
    } as RuntimeFactory;
    Object.defineProperty(wrapped, "retromDOSBoxStateHook", { value: true });
    return wrapped;
  };

  const instantiate = (async (source: BufferSource | WebAssembly.Module, imports?: WebAssembly.Imports) => {
    if (source instanceof target.WebAssembly.Module) {return originalInstantiate(source, imports);}
    const patched = patchDOSBoxPureStateStack(source);
    if (!patched) {return originalInstantiate(source, imports);}
    patchedArtifact = true;
    if (managerConstructor) {patchManager(managerConstructor);}
    return originalInstantiate(patched, imports);
  }) as typeof WebAssembly.instantiate;
  target.WebAssembly.instantiate = instantiate;
  if (originalInstantiateStreaming) {
    target.WebAssembly.instantiateStreaming = async (
      source: Response | PromiseLike<Response>, imports?: WebAssembly.Imports,
    ) => {
      const response = await source;
      const bytes = new Uint8Array(await response.clone().arrayBuffer());
      const patched = patchDOSBoxPureStateStack(bytes);
      if (!patched) {return originalInstantiateStreaming(response, imports);}
      patchedArtifact = true;
      if (managerConstructor) {patchManager(managerConstructor);}
      return originalInstantiate(patched, imports) as Promise<WebAssembly.WebAssemblyInstantiatedSource>;
    };
  }

  if (managerConstructor) {patchManager(managerConstructor);}
  Object.defineProperty(target, "EJS_GameManager", {
    configurable: true,
    enumerable: managerDescriptor?.enumerable ?? true,
    get: () => managerConstructor,
    set: (constructor: GameManagerConstructor | undefined) => {
      patchManager(constructor);
      managerConstructor = constructor;
    },
  });
  if (runtimeFactory) {runtimeFactory = wrapRuntime(runtimeFactory);}
  Object.defineProperty(target, "EJS_Runtime", {
    configurable: true,
    enumerable: runtimeDescriptor?.enumerable ?? true,
    get: () => runtimeFactory,
    set: (factory: RuntimeFactory | undefined) => { runtimeFactory = wrapRuntime(factory); },
  });

  const cleanup = () => {
    if (!active) {return;}
    active = false;
    target.WebAssembly.instantiate = originalInstantiate;
    if (originalInstantiateStreaming) {target.WebAssembly.instantiateStreaming = originalInstantiateStreaming;}
    for (const prototype of patchedPrototypes) {
      const descriptors = prototypeDescriptors.get(prototype);
      if (descriptors?.getState) {Object.defineProperty(prototype, "getState", descriptors.getState);}
      else {Reflect.deleteProperty(prototype, "getState");}
      if (descriptors?.loadExplicitStateAndWait) {
        Object.defineProperty(prototype, "loadExplicitStateAndWait", descriptors.loadExplicitStateAndWait);
      } else {Reflect.deleteProperty(prototype, "loadExplicitStateAndWait");}
    }
    if (managerDescriptor) {Object.defineProperty(target, "EJS_GameManager", managerDescriptor);}
    else {Reflect.deleteProperty(target, "EJS_GameManager");}
    if (runtimeDescriptor) {Object.defineProperty(target, "EJS_Runtime", runtimeDescriptor);}
    else {Reflect.deleteProperty(target, "EJS_Runtime");}
    for (const signal of waitSignals) {
      target.clearTimeout(signal.timer);
      signal.reject(new Error("PLAYER_SESSION_ENDED"));
    }
    waitSignals.clear();
    for (const signal of loadSignals.splice(0)) {
      target.clearTimeout(signal.timer);
      signal.reject(new Error("PLAYER_SESSION_ENDED"));
    }
    for (const signal of mainLoopSignals.splice(0)) {
      target.clearTimeout(signal.timer);
      signal.reject(new Error("PLAYER_SESSION_ENDED"));
    }
  };
  return {
    prepare(instance: EmulatorInstance) {
      const manager = instance.gameManager as DOSBoxManager | undefined;
      const prototype = manager ? Object.getPrototypeOf(manager) as DOSBoxManager | null : null;
      if (!manager || !prototype) {throw new Error("PLAYER_DOS_STATE_COMPATIBILITY_UNAVAILABLE");}
      patchManager({ prototype });
    },
    cleanup,
  };
}
