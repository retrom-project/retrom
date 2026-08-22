import { describe, expect, it } from "vitest";
import {
  clearTransientSaveStorage,
  isTransientSaveFileSystem,
  TRANSIENT_SAVE_ROOT,
  type TransientSaveFileSystem,
} from "./transient-save-storage";

const directoryMode = 0o040777;
const fileMode = 0o100666;

function fixture() {
  const files = new Set<string>();
  const directories = new Set(["/", "/data", TRANSIENT_SAVE_ROOT]);
  const children = (path: string) => {
    const prefix = path === "/" ? "/" : `${path}/`;
    const names = new Set<string>();
    for (const candidate of [...directories, ...files]) {
      if (!candidate.startsWith(prefix)) continue;
      const suffix = candidate.slice(prefix.length);
      if (suffix && !suffix.includes("/")) names.add(suffix);
    }
    return [".", "..", ...Array.from(names).sort()];
  };
  const fileSystem: TransientSaveFileSystem = {
    analyzePath: (path) => ({ exists: directories.has(path) || files.has(path) }),
    mkdir: (path) => { directories.add(path); },
    unlink: (path) => { files.delete(path); },
    readdir: children,
    lstat: (path) => {
      if (files.has(path)) return { mode: fileMode };
      if (directories.has(path)) return { mode: directoryMode };
      throw new Error("missing path");
    },
    rmdir: (path) => { directories.delete(path); },
  };
  const write = (relativePath: string) => {
    let current = TRANSIENT_SAVE_ROOT;
    for (const segment of relativePath.split("/").slice(0, -1)) {
      current += `/${segment}`;
      directories.add(current);
    }
    files.add(`${TRANSIENT_SAVE_ROOT}/${relativePath}`);
  };
  return { directories, files, fileSystem, write };
}

describe("transient EmulatorJS save storage", () => {
  it("recognizes only the filesystem operations required by the launch boundary", () => {
    const target = fixture();
    expect(isTransientSaveFileSystem(target.fileSystem)).toBe(true);
    expect(isTransientSaveFileSystem({ analyzePath: () => ({ exists: true }) })).toBe(false);
  });

  it("clears all nested files before a fresh or save-state launch", () => {
    const target = fixture();
    target.write("PSP/SAVEDATA/GAME/DATA.BIN");
    target.write("mgba/game.srm");

    clearTransientSaveStorage(target.fileSystem);

    expect(target.files.size).toBe(0);
    expect(target.directories).toEqual(new Set(["/", "/data", TRANSIENT_SAVE_ROOT]));
  });

  it("recreates a missing save mount", () => {
    const target = fixture();
    target.directories.delete(TRANSIENT_SAVE_ROOT);

    clearTransientSaveStorage(target.fileSystem);

    expect(target.directories.has(TRANSIENT_SAVE_ROOT)).toBe(true);
  });

  it("rejects symbolic links instead of following them", () => {
    const target = fixture();
    target.write("unsafe-link");
    target.fileSystem.lstat = () => ({ mode: 0o120777 });

    expect(() => clearTransientSaveStorage(target.fileSystem)).toThrow("LAUNCH_TRANSIENT_SAVE_CLEAR_FAILED");
  });
});
