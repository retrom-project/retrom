const VERSION_URL = "https://cdn.emulatorjs.org/stable/data/version.json";

export function installEmulatorJs423NetplayPatch(target = window) {
  const report = {
    version: "4.2.3",
    managerPatched: false,
    inMemorySaves: false,
    offlineVersionCheck: false,
    frameHookBootstrap: false,
    errors: []
  };

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
    report.managerPatched = true;
    report.inMemorySaves = true;
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
        postMainLoop() {
          if (typeof originalPostMainLoop === "function") originalPostMainLoop();
          if (typeof target.__RETROM_POST_MAIN_LOOP__ === "function") {
            target.__RETROM_POST_MAIN_LOOP__();
          }
        }
      };
      report.frameHookBootstrap = true;
      return Reflect.apply(factory, this, [patchedConfig]);
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
