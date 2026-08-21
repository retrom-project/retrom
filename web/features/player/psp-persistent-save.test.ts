import { describe, expect, it, vi } from "vitest";
import {
  fingerprintPspSaveTree,
  hasRetromSaveEnvelopePrefix,
  isPersistentSaveTreeBundle,
  PERSISTENT_SAVE_ROOT,
  PSP_SAVE_ROOT,
  PspPersistentSaveSync,
  restorePspSaveTree,
  snapshotPspSaveTree,
  type PspSaveFileSystem,
} from "./psp-persistent-save";

const directoryMode = 0o040777;
const fileMode = 0o100666;

function fixture() {
  let clock = 1;
  const files = new Map<string, { bytes: Uint8Array; mtime: number }>();
  const directories = new Set(["/", "/data", "/data/saves"]);
  const children = (path: string) => {
    const prefix = path === "/" ? "/" : `${path}/`;
    const names = new Set<string>();
    for (const candidate of [...directories, ...files.keys()]) {
      if (!candidate.startsWith(prefix)) continue;
      const suffix = candidate.slice(prefix.length);
      if (suffix && !suffix.includes("/")) names.add(suffix);
    }
    return [".", "..", ...Array.from(names).sort()];
  };
  const fileSystem: PspSaveFileSystem = {
    analyzePath: (path) => ({ exists: directories.has(path) || files.has(path) }),
    mkdir: (path) => { directories.add(path); },
    writeFile: (path, bytes) => { files.set(path, { bytes: Uint8Array.from(bytes), mtime: clock++ }); },
    unlink: (path) => { files.delete(path); },
    readdir: (path) => children(path),
    readFile: (path) => {
      const file = files.get(path);
      if (!file) throw new Error("missing file");
      return file.bytes;
    },
    stat: (path) => {
      const file = files.get(path);
      if (file) return { mode: fileMode, size: file.bytes.byteLength, mtime: file.mtime };
      if (directories.has(path)) return { mode: directoryMode, size: 0, mtime: 0 };
      throw new Error("missing path");
    },
    lstat: (path) => {
      const file = files.get(path);
      if (file) return { mode: fileMode, size: file.bytes.byteLength, mtime: file.mtime };
      if (directories.has(path)) return { mode: directoryMode, size: 0, mtime: 0 };
      throw new Error("missing path");
    },
    rmdir: (path) => { directories.delete(path); },
  };
  const write = (relativePath: string, bytes: number[]) => {
    let current = PERSISTENT_SAVE_ROOT;
    for (const segment of `PSP/SAVEDATA/${relativePath}`.split("/").slice(0, -1)) {
      current += `/${segment}`;
      directories.add(current);
    }
    fileSystem.writeFile(`${PSP_SAVE_ROOT}/${relativePath}`, Uint8Array.from(bytes));
  };
  return { directories, fileSystem, files, write };
}

describe("PSP persistent save bundle", () => {
  it("distinguishes current and PPSSPP-only legacy envelopes from old single-file bytes", () => {
    const source = fixture();
    source.write("GAME/DATA.BIN", [1]);
    const current = snapshotPspSaveTree(source.fileSystem, PSP_SAVE_ROOT);
    const legacy = new Uint8Array(current);
    legacy.set(new TextEncoder().encode("RETPSP01"));

    expect(isPersistentSaveTreeBundle(current)).toBe(true);
    expect(isPersistentSaveTreeBundle(legacy)).toBe(false);
    expect(isPersistentSaveTreeBundle(legacy, true)).toBe(true);
    expect(isPersistentSaveTreeBundle(Uint8Array.of(1, 2, 3))).toBe(false);
    expect(hasRetromSaveEnvelopePrefix(new TextEncoder().encode("RETFS000"))).toBe(true);
    expect(hasRetromSaveEnvelopePrefix(Uint8Array.of(1, 2, 3))).toBe(false);
  });

  it("captures a deterministic tree and restores it after clearing browser residue", () => {
    const source = fixture();
    source.write("ULUS12345/DATA.BIN", [1, 2, 3]);
    source.write("ULUS12345/ICON0.PNG", [8, 9]);
    const bundle = snapshotPspSaveTree(source.fileSystem);

    const target = fixture();
    target.write("STALE/OLD.BIN", [7]);
    restorePspSaveTree(target.fileSystem, bundle);

    expect(Array.from(target.files.keys()).sort()).toEqual([
      `${PSP_SAVE_ROOT}/ULUS12345/DATA.BIN`,
      `${PSP_SAVE_ROOT}/ULUS12345/ICON0.PNG`,
    ]);
    expect(target.files.get(`${PSP_SAVE_ROOT}/ULUS12345/DATA.BIN`)?.bytes).toEqual(Uint8Array.of(1, 2, 3));
    expect(snapshotPspSaveTree(target.fileSystem)).toEqual(bundle);
  });

  it("clears the complete save mount when the launch has no server revision", () => {
    const target = fixture();
    target.write("OTHER_GAME/DATA.BIN", [4]);
    restorePspSaveTree(target.fileSystem, null);
    expect(target.files.size).toBe(0);
    expect(target.directories.has(PERSISTENT_SAVE_ROOT)).toBe(true);
  });

  it("rejects traversal paths before touching the mounted filesystem", () => {
    const target = fixture();
    target.write("SAFE/DATA.BIN", [1]);
    const path = new TextEncoder().encode("../escape.bin");
    const bundle = new Uint8Array(12 + 6 + path.byteLength + 1);
    bundle.set(new TextEncoder().encode("RETFS001"));
    const view = new DataView(bundle.buffer);
    view.setUint32(8, 1, true);
    view.setUint16(12, path.byteLength, true);
    view.setUint32(14, 1, true);
    bundle.set(path, 18);
    bundle[bundle.byteLength - 1] = 9;

    expect(() => restorePspSaveTree(target.fileSystem, bundle)).toThrow("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
    expect(target.files.has(`${PSP_SAVE_ROOT}/SAFE/DATA.BIN`)).toBe(true);
  });

  it("changes its fingerprint for same-size writes", () => {
    const target = fixture();
    target.write("GAME/DATA.BIN", [1, 2]);
    const before = fingerprintPspSaveTree(target.fileSystem);
    target.write("GAME/DATA.BIN", [3, 4]);
    expect(fingerprintPspSaveTree(target.fileSystem).value).not.toBe(before.value);
  });

  it("rejects symbolic links instead of following them outside SAVEDATA", () => {
    const target = fixture();
    target.write("GAME/LINK.BIN", [1]);
    target.fileSystem.lstat = () => ({ mode: 0o120777, size: 1, mtime: 1 });
    expect(() => snapshotPspSaveTree(target.fileSystem)).toThrow("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
  });

  it("encodes a PPSSPP-scoped snapshot relative to the shared save mount", () => {
    const source = fixture();
    source.write("GAME/DATA.BIN", [1, 2, 3]);
    source.directories.add(`${PERSISTENT_SAVE_ROOT}/PSP/GAME`);
    source.fileSystem.writeFile(`${PERSISTENT_SAVE_ROOT}/PSP/GAME/CACHE.BIN`, Uint8Array.of(9));

    const bundle = snapshotPspSaveTree(source.fileSystem, PSP_SAVE_ROOT);
    const target = fixture();
    restorePspSaveTree(target.fileSystem, bundle);

    expect(Array.from(target.files.keys())).toEqual([`${PSP_SAVE_ROOT}/GAME/DATA.BIN`]);
  });

  it("excludes a configured cache subtree from generic snapshots and fingerprints", () => {
    const source = fixture();
    for (const directory of [
      "/data/saves/Azahar",
      "/data/saves/Azahar/Azahar",
      "/data/saves/Azahar/Azahar/nand",
      "/data/saves/Azahar/Azahar/shaders",
    ]) source.directories.add(directory);
    source.fileSystem.writeFile("/data/saves/Azahar/Azahar/nand/save.bin", Uint8Array.of(1, 2));
    source.fileSystem.writeFile("/data/saves/Azahar/Azahar/shaders/cache.bin", Uint8Array.of(3, 4));
    const excluded = ["Azahar/Azahar/shaders"];
    const fingerprint = fingerprintPspSaveTree(source.fileSystem, PERSISTENT_SAVE_ROOT, excluded);
    const bundle = snapshotPspSaveTree(source.fileSystem, PERSISTENT_SAVE_ROOT, excluded);

    source.fileSystem.writeFile("/data/saves/Azahar/Azahar/shaders/cache.bin", Uint8Array.of(9, 9));
    expect(fingerprintPspSaveTree(source.fileSystem, PERSISTENT_SAVE_ROOT, excluded)).toEqual(fingerprint);

    const target = fixture();
    restorePspSaveTree(target.fileSystem, bundle);
    expect(Array.from(target.files.keys())).toEqual(["/data/saves/Azahar/Azahar/nand/save.bin"]);
  });
});

describe("PspPersistentSaveSync", () => {
  it("waits for a stable change, pauses around capture, and uploads automatically", async () => {
    const target = fixture();
    restorePspSaveTree(target.fileSystem, null);
    const loop = vi.fn();
    const upload = vi.fn<(bytes: Uint8Array, event: "AUTO_INTERVAL" | "EXIT") => Promise<boolean>>()
      .mockResolvedValue(true);
    const sync = new PspPersistentSaveSync(target.fileSystem, { toggleMainLoop: loop }, upload, { stableMs: 2_000 });
    target.write("GAME/DATA.BIN", [1, 2, 3]);

    expect(await sync.poll(1_000)).toBe(false);
    expect(await sync.poll(2_999)).toBe(false);
    expect(await sync.poll(3_000)).toBe(true);

    expect(loop.mock.calls).toEqual([[false], [true]]);
    expect(upload).toHaveBeenCalledTimes(1);
    expect(upload.mock.calls[0]?.[1]).toBe("AUTO_INTERVAL");
  });

  it("uploads an empty bundle on exit when the user deleted an existing save", async () => {
    const target = fixture();
    target.write("GAME/DATA.BIN", [1]);
    const upload = vi.fn<(bytes: Uint8Array, event: "AUTO_INTERVAL" | "EXIT") => Promise<boolean>>()
      .mockResolvedValue(true);
    const sync = new PspPersistentSaveSync(target.fileSystem, { toggleMainLoop: vi.fn() }, upload);
    target.fileSystem.unlink(`${PSP_SAVE_ROOT}/GAME/DATA.BIN`);

    expect(await sync.flush()).toBe(true);
    expect(upload.mock.calls[0]?.[0]).toHaveLength(12);
    expect(upload.mock.calls[0]?.[1]).toBe("EXIT");
  });

  it("does not resume a game that the user already paused", async () => {
    const target = fixture();
    target.write("GAME/DATA.BIN", [1]);
    const loop = vi.fn();
    const sync = new PspPersistentSaveSync(
      target.fileSystem,
      { toggleMainLoop: loop },
      async () => true,
      { isPaused: () => true },
    );
    expect(await sync.flush()).toBe(true);
    expect(loop.mock.calls).toEqual([[false], [false]]);
  });

  it("runs the opt-in reset and native save flush before an exit snapshot", async () => {
    const target = fixture();
    const calls: string[] = [];
    const sync = new PspPersistentSaveSync(target.fileSystem, {
      toggleMainLoop: () => undefined,
      functions: { restart: () => { calls.push("restart"); target.write("GAME/DATA.BIN", [1]); } },
      saveSaveFiles: () => { calls.push("save-files"); },
    }, async () => true, { restartOnExit: true });

    expect(await sync.flush()).toBe(true);
    expect(calls).toEqual(["restart", "save-files"]);
  });
});
