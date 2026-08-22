export const TRANSIENT_SAVE_ROOT = "/data/saves";

const fileTypeMask = 0o170000;
const directoryType = 0o040000;
const regularFileType = 0o100000;

export type TransientSaveFileSystem = {
  analyzePath: (path: string) => { exists: boolean };
  mkdir: (path: string) => void;
  unlink: (path: string) => void;
  readdir: (path: string) => string[];
  lstat: (path: string) => { mode: number };
  rmdir: (path: string) => void;
  isDir?: (mode: number) => boolean;
  isFile?: (mode: number) => boolean;
};

export function isTransientSaveFileSystem(value: unknown): value is TransientSaveFileSystem {
  if (typeof value !== "object" || value === null) {return false;}
  return ["analyzePath", "mkdir", "unlink", "readdir", "lstat", "rmdir"]
    .every((name) => typeof Reflect.get(value, name) === "function");
}

function isDirectory(fileSystem: TransientSaveFileSystem, mode: number) {
  return fileSystem.isDir?.(mode) ?? (mode & fileTypeMask) === directoryType;
}

function isRegularFile(fileSystem: TransientSaveFileSystem, mode: number) {
  return fileSystem.isFile?.(mode) ?? (mode & fileTypeMask) === regularFileType;
}

function ensureDirectory(fileSystem: TransientSaveFileSystem, directoryPath: string) {
  let current = "";
  for (const segment of directoryPath.split("/").filter(Boolean)) {
    current += `/${segment}`;
    if (!fileSystem.analyzePath(current).exists) {fileSystem.mkdir(current);}
  }
}

function removeTree(fileSystem: TransientSaveFileSystem, directoryPath: string) {
  if (!fileSystem.analyzePath(directoryPath).exists) {return;}
  for (const name of fileSystem.readdir(directoryPath)) {
    if (name === "." || name === "..") {continue;}
    const childPath = `${directoryPath}/${name}`;
    const stat = fileSystem.lstat(childPath);
    if (isDirectory(fileSystem, stat.mode)) {
      removeTree(fileSystem, childPath);
      fileSystem.rmdir(childPath);
    } else if (isRegularFile(fileSystem, stat.mode)) {
      fileSystem.unlink(childPath);
    } else {
      throw new Error("LAUNCH_TRANSIENT_SAVE_CLEAR_FAILED");
    }
  }
}

/** Clears EmulatorJS' locally persisted save mount before every launch. */
export function clearTransientSaveStorage(fileSystem: TransientSaveFileSystem) {
  try {
    removeTree(fileSystem, TRANSIENT_SAVE_ROOT);
    ensureDirectory(fileSystem, TRANSIENT_SAVE_ROOT);
  } catch {
    throw new Error("LAUNCH_TRANSIENT_SAVE_CLEAR_FAILED");
  }
}
