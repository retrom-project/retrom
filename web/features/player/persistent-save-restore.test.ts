import { describe, expect, it, vi } from "vitest";
import { injectPersistentSave, restorePersistentSave, type PersistentSaveFileSystem, type PersistentSaveManager } from "./persistent-save-restore";

function fixture(existingPaths: string[] = []) {
  const paths = new Set(existingPaths);
  const calls: string[] = [];
  const fileSystem: PersistentSaveFileSystem = {
    analyzePath: vi.fn((path) => ({ exists: paths.has(path) })),
    mkdir: vi.fn((path) => { calls.push(`mkdir:${path}`); paths.add(path); }),
    writeFile: vi.fn((path) => { calls.push(`write:${path}`); paths.add(path); }),
    unlink: vi.fn((path) => { calls.push(`unlink:${path}`); paths.delete(path); })
  };
  const manager: PersistentSaveManager = {
    loadSaveFiles: vi.fn(() => { calls.push("reload"); }),
    toggleMainLoop: vi.fn((running) => { calls.push(`loop:${running}`); })
  };
  return { calls, fileSystem, manager };
}

describe("restorePersistentSave", () => {
  it("overwrites IDBFS with the launch-locked server revision before resuming", () => {
    const { calls, fileSystem, manager } = fixture(["/data", "/data/saves"]);
    const bytes = Uint8Array.from([1, 2, 3]);
    restorePersistentSave(manager, fileSystem, "/data/saves/game.srm", bytes);
    expect(fileSystem.writeFile).toHaveBeenCalledWith("/data/saves/game.srm", bytes);
    expect(fileSystem.unlink).not.toHaveBeenCalled();
    expect(calls).toEqual(["loop:false", "write:/data/saves/game.srm", "reload", "loop:true"]);
  });

  it("deletes a browser residue when the current account has no server save", () => {
    const { calls, fileSystem, manager } = fixture(["/data", "/data/saves", "/data/saves/game.srm"]);
    restorePersistentSave(manager, fileSystem, "/data/saves/game.srm", null);
    expect(fileSystem.unlink).toHaveBeenCalledWith("/data/saves/game.srm");
    expect(calls).toEqual(["loop:false", "unlink:/data/saves/game.srm", "reload", "loop:true"]);
  });

  it("fails closed when the runtime cannot reload saves", () => {
    const { fileSystem, manager } = fixture(["/data", "/data/saves"]);
    manager.loadSaveFiles = undefined;
    expect(() => restorePersistentSave(manager, fileSystem, "/data/saves/game.srm", null))
      .toThrow("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
    expect(manager.toggleMainLoop).toHaveBeenCalledTimes(1);
    expect(manager.toggleMainLoop).toHaveBeenLastCalledWith(false);
  });

  it("normalizes filesystem failures and never resumes the main loop", () => {
    const { fileSystem, manager } = fixture(["/data", "/data/saves"]);
    vi.mocked(fileSystem.writeFile).mockImplementation(() => { throw new Error("browser path leaked"); });
    expect(() => restorePersistentSave(manager, fileSystem, "/data/saves/game.srm", Uint8Array.of(1)))
      .toThrow("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
    expect(manager.toggleMainLoop).toHaveBeenCalledTimes(1);
    expect(manager.toggleMainLoop).toHaveBeenLastCalledWith(false);
  });

  it("injects persistent bytes without changing a caller-owned pause boundary", () => {
    const { calls, fileSystem, manager } = fixture(["/data", "/data/saves"]);
    injectPersistentSave(manager, fileSystem, "/data/saves/game.srm", Uint8Array.of(7));
    expect(calls).toEqual(["write:/data/saves/game.srm", "reload"]);
    expect(manager.toggleMainLoop).not.toHaveBeenCalled();
  });
});
