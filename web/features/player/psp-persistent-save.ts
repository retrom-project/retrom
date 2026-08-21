export const PERSISTENT_SAVE_ROOT = "/data/saves";
export const PSP_SAVE_ROOT = `${PERSISTENT_SAVE_ROOT}/PSP/SAVEDATA`;
export const PERSISTENT_SAVE_BUNDLE_MAX_BYTES = 64 * 1024 * 1024;
export const PSP_SAVE_BUNDLE_MAX_BYTES = PERSISTENT_SAVE_BUNDLE_MAX_BYTES;

const bundleMagic = Uint8Array.of(0x52, 0x45, 0x54, 0x46, 0x53, 0x30, 0x30, 0x31); // RETFS001
const legacyPspBundleMagic = Uint8Array.of(0x52, 0x45, 0x54, 0x50, 0x53, 0x50, 0x30, 0x31); // RETPSP01
const bundleHeaderBytes = bundleMagic.byteLength + 4;
const entryHeaderBytes = 2 + 4;
const maximumEntries = 4_096;
const maximumPathBytes = 1_024;
const fileTypeMask = 0o170000;
const directoryType = 0o040000;
const regularFileType = 0o100000;

export type PspSaveStat = {
  mode: number;
  size: number;
  mtime?: Date | number;
  ctime?: Date | number;
};

export type PspSaveFileSystem = {
  analyzePath: (path: string) => { exists: boolean };
  mkdir: (path: string) => void;
  writeFile: (path: string, bytes: Uint8Array) => void;
  unlink: (path: string) => void;
  readdir: (path: string) => string[];
  readFile: (path: string) => ArrayBufferView;
  stat: (path: string) => PspSaveStat;
  lstat: (path: string) => PspSaveStat;
  rmdir: (path: string) => void;
  isDir?: (mode: number) => boolean;
  isFile?: (mode: number) => boolean;
};

export type PersistentSaveTreeFileSystem = PspSaveFileSystem;

export function isPspSaveFileSystem(value: unknown): value is PspSaveFileSystem {
  if (typeof value !== "object" || value === null) return false;
  return ["analyzePath", "mkdir", "writeFile", "unlink", "readdir", "readFile", "stat", "lstat", "rmdir"]
    .every((name) => typeof Reflect.get(value, name) === "function");
}

export const isPersistentSaveTreeFileSystem = isPspSaveFileSystem;

export function isPersistentSaveTreeBundle(bundle: Uint8Array, allowLegacyPsp = false) {
  const currentFormat = bundle.byteLength >= bundleMagic.byteLength &&
    bundleMagic.every((byte, index) => bundle[index] === byte);
  const legacyPspFormat = allowLegacyPsp && bundle.byteLength >= legacyPspBundleMagic.byteLength &&
    legacyPspBundleMagic.every((byte, index) => bundle[index] === byte);
  return currentFormat || legacyPspFormat;
}

export function hasRetromSaveEnvelopePrefix(bundle: Uint8Array) {
  return bundle.byteLength >= 3 && bundle[0] === 0x52 && bundle[1] === 0x45 && bundle[2] === 0x54;
}

type SaveEntry = {
  relativePath: string;
  encodedPath: Uint8Array;
  size: number;
};

export type PspSaveTreeFingerprint = {
  value: string;
  fileCount: number;
};
export type PersistentSaveTreeFingerprint = PspSaveTreeFingerprint;

export type PspSaveManager = {
  toggleMainLoop?: (running: boolean) => void;
  saveSaveFiles?: () => void;
  functions?: { restart?: () => void };
};
export type PersistentSaveTreeManager = PspSaveManager;

type SyncEvent = "AUTO_INTERVAL" | "EXIT";

type SyncOptions = {
  intervalMs?: number;
  stableMs?: number;
  isPaused?: () => boolean;
  onError?: (error: Error) => void;
  restartOnExit?: boolean;
  captureRoot?: string;
  excludedBundlePaths?: string[];
};

function normalizedError() {
  return new Error("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
}

function isDirectory(fileSystem: PspSaveFileSystem, mode: number) {
  return fileSystem.isDir?.(mode) ?? (mode & fileTypeMask) === directoryType;
}

function isRegularFile(fileSystem: PspSaveFileSystem, mode: number) {
  return fileSystem.isFile?.(mode) ?? (mode & fileTypeMask) === regularFileType;
}

function encodePath(relativePath: string) {
  if (!relativePath || relativePath.startsWith("/") || relativePath.includes("\\") || relativePath.includes("\0")) {
    throw normalizedError();
  }
  const segments = relativePath.split("/");
  if (segments.some((segment) => !segment || segment === "." || segment === ".." ||
    Array.from(segment).some((character) => {
      const codePoint = character.codePointAt(0) ?? 0;
      return codePoint <= 0x1f || codePoint >= 0x7f && codePoint <= 0x9f;
    }))) throw normalizedError();
  const encoded = new TextEncoder().encode(relativePath);
  if (!encoded.byteLength || encoded.byteLength > maximumPathBytes) throw normalizedError();
  return encoded;
}

function compareBytes(left: Uint8Array, right: Uint8Array) {
  const length = Math.min(left.byteLength, right.byteLength);
  for (let index = 0; index < length; index += 1) {
    if (left[index] !== right[index]) return left[index] - right[index];
  }
  return left.byteLength - right.byteLength;
}

function stableTimestamp(stat: PspSaveStat) {
  for (const value of [stat.mtime, stat.ctime]) {
    if (value && typeof value === "object" && "getTime" in value && typeof value.getTime === "function") {
      const timestamp = value.getTime();
      if (Number.isFinite(timestamp)) return timestamp;
    }
    if (typeof value === "number" && Number.isFinite(value)) return value;
  }
  return null;
}

function hashView(view: ArrayBufferView) {
  const bytes = new Uint8Array(view.buffer, view.byteOffset, view.byteLength);
  let hash = 0x811c9dc5;
  for (const byte of bytes) {
    hash ^= byte;
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16).padStart(8, "0");
}

function listEntries(
  fileSystem: PspSaveFileSystem,
  root = PERSISTENT_SAVE_ROOT,
  excludedBundlePaths: readonly string[] = [],
) {
  if (!fileSystem.analyzePath(root).exists) return [];
  const entries: SaveEntry[] = [];
  const pending = [{ absolutePath: root, relativePath: "" }];
  let visited = 0;
  while (pending.length) {
    const directory = pending.pop();
    if (!directory) break;
    for (const name of fileSystem.readdir(directory.absolutePath)) {
      if (name === "." || name === "..") continue;
      visited += 1;
      if (visited > maximumEntries * 2) throw normalizedError();
      const relativePath = directory.relativePath ? `${directory.relativePath}/${name}` : name;
      const bundlePath = root === PERSISTENT_SAVE_ROOT
        ? relativePath
        : `${root.slice(PERSISTENT_SAVE_ROOT.length + 1)}/${relativePath}`;
      if (excludedBundlePaths.some((path) => bundlePath === path || bundlePath.startsWith(`${path}/`))) continue;
      const encodedPath = encodePath(bundlePath);
      const absolutePath = `${directory.absolutePath}/${name}`;
      const stat = fileSystem.lstat(absolutePath);
      if (!Number.isSafeInteger(stat.size) || stat.size < 0) throw normalizedError();
      if (isDirectory(fileSystem, stat.mode)) {
        pending.push({ absolutePath, relativePath });
      } else if (isRegularFile(fileSystem, stat.mode)) {
        entries.push({ relativePath, encodedPath, size: stat.size });
        if (entries.length > maximumEntries) throw normalizedError();
      } else {
        throw normalizedError();
      }
    }
  }
  entries.sort((left, right) => compareBytes(left.encodedPath, right.encodedPath));
  return entries;
}

function ensureDirectory(fileSystem: PspSaveFileSystem, directoryPath: string) {
  let current = "";
  for (const segment of directoryPath.split("/").filter(Boolean)) {
    current += `/${segment}`;
    if (!fileSystem.analyzePath(current).exists) fileSystem.mkdir(current);
  }
}

function removeTree(fileSystem: PspSaveFileSystem, directoryPath: string) {
  if (!fileSystem.analyzePath(directoryPath).exists) return;
  for (const name of fileSystem.readdir(directoryPath)) {
    if (name === "." || name === "..") continue;
    const childPath = `${directoryPath}/${name}`;
    const stat = fileSystem.lstat(childPath);
    if (isDirectory(fileSystem, stat.mode)) {
      removeTree(fileSystem, childPath);
      fileSystem.rmdir(childPath);
    } else if (isRegularFile(fileSystem, stat.mode)) {
      fileSystem.unlink(childPath);
    } else {
      throw normalizedError();
    }
  }
}

export function snapshotPspSaveTree(
  fileSystem: PspSaveFileSystem,
  root = PERSISTENT_SAVE_ROOT,
  excludedBundlePaths: readonly string[] = [],
) {
  try {
    const entries = listEntries(fileSystem, root, excludedBundlePaths);
    let totalBytes = bundleHeaderBytes;
    for (const entry of entries) {
      totalBytes += entryHeaderBytes + entry.encodedPath.byteLength + entry.size;
      if (totalBytes > PERSISTENT_SAVE_BUNDLE_MAX_BYTES) throw new Error("LAUNCH_PERSISTENT_SAVE_TOO_LARGE");
    }
    const result = new Uint8Array(totalBytes);
    result.set(bundleMagic, 0);
    const data = new DataView(result.buffer);
    data.setUint32(bundleMagic.byteLength, entries.length, true);
    let offset = bundleHeaderBytes;
    for (const entry of entries) {
      data.setUint16(offset, entry.encodedPath.byteLength, true);
      data.setUint32(offset + 2, entry.size, true);
      offset += entryHeaderBytes;
      result.set(entry.encodedPath, offset);
      offset += entry.encodedPath.byteLength;
      const source = fileSystem.readFile(`${root}/${entry.relativePath}`);
      if (source.byteLength !== entry.size) throw normalizedError();
      result.set(new Uint8Array(source.buffer, source.byteOffset, source.byteLength), offset);
      offset += source.byteLength;
    }
    return result;
  } catch (error) {
    if (error instanceof Error && error.message === "LAUNCH_PERSISTENT_SAVE_TOO_LARGE") throw error;
    throw normalizedError();
  }
}

export function restorePspSaveTree(fileSystem: PspSaveFileSystem, bundle: Uint8Array | null) {
  try {
    const entries: Array<{ relativePath: string; bytes: Uint8Array }> = [];
    let entryRoot = PERSISTENT_SAVE_ROOT;
    if (bundle) {
      if (bundle.byteLength < bundleHeaderBytes || bundle.byteLength > PERSISTENT_SAVE_BUNDLE_MAX_BYTES) {
        throw normalizedError();
      }
      const currentFormat = bundleMagic.every((byte, index) => bundle[index] === byte);
      const legacyPspFormat = legacyPspBundleMagic.every((byte, index) => bundle[index] === byte);
      if (!currentFormat && !legacyPspFormat) throw normalizedError();
      if (legacyPspFormat) entryRoot = PSP_SAVE_ROOT;
      const data = new DataView(bundle.buffer, bundle.byteOffset, bundle.byteLength);
      const entryCount = data.getUint32(bundleMagic.byteLength, true);
      if (entryCount > maximumEntries) throw normalizedError();
      let offset = bundleHeaderBytes;
      let previousPath: Uint8Array | null = null;
      const decoder = new TextDecoder("utf-8", { fatal: true });
      for (let index = 0; index < entryCount; index += 1) {
        if (offset + entryHeaderBytes > bundle.byteLength) throw normalizedError();
        const pathLength = data.getUint16(offset, true);
        const fileLength = data.getUint32(offset + 2, true);
        offset += entryHeaderBytes;
        if (!pathLength || pathLength > maximumPathBytes || offset + pathLength + fileLength > bundle.byteLength) {
          throw normalizedError();
        }
        const encodedPath = bundle.subarray(offset, offset + pathLength);
        if (previousPath && compareBytes(previousPath, encodedPath) >= 0) throw normalizedError();
        const relativePath = decoder.decode(encodedPath);
        if (compareBytes(encodePath(relativePath), encodedPath) !== 0) throw normalizedError();
        offset += pathLength;
        entries.push({ relativePath, bytes: bundle.subarray(offset, offset + fileLength) });
        offset += fileLength;
        previousPath = encodedPath;
      }
      if (offset !== bundle.byteLength) throw normalizedError();
    }

    removeTree(fileSystem, PERSISTENT_SAVE_ROOT);
    ensureDirectory(fileSystem, entryRoot);
    for (const entry of entries) {
      const absolutePath = `${entryRoot}/${entry.relativePath}`;
      ensureDirectory(fileSystem, absolutePath.slice(0, absolutePath.lastIndexOf("/")));
      fileSystem.writeFile(absolutePath, entry.bytes);
    }
  } catch {
    throw normalizedError();
  }
}

export function fingerprintPspSaveTree(
  fileSystem: PspSaveFileSystem,
  root = PERSISTENT_SAVE_ROOT,
  excludedBundlePaths: readonly string[] = [],
): PspSaveTreeFingerprint {
  try {
    const entries = listEntries(fileSystem, root, excludedBundlePaths);
    const values = entries.map((entry) => {
      const stat = fileSystem.lstat(`${root}/${entry.relativePath}`);
      const timestamp = stableTimestamp(stat);
      const contentMarker = timestamp === null
        ? hashView(fileSystem.readFile(`${root}/${entry.relativePath}`))
        : String(timestamp);
      return `${entry.relativePath.length}:${entry.relativePath}:${entry.size}:${contentMarker}`;
    });
    return { value: values.join("\n"), fileCount: entries.length };
  } catch {
    throw normalizedError();
  }
}

export class PspPersistentSaveSync {
  private readonly intervalMs: number;
  private readonly stableMs: number;
  private readonly isPaused: () => boolean;
  private readonly onError: (error: Error) => void;
  private saved: PspSaveTreeFingerprint;
  private candidate: { fingerprint: PspSaveTreeFingerprint; observedAt: number } | null = null;
  private timer: ReturnType<typeof setInterval> | null = null;
  private active: Promise<boolean> | null = null;
  private failedFingerprint: string | null = null;
  private readonly restartOnExit: boolean;
  private readonly captureRoot: string;
  private readonly excludedBundlePaths: readonly string[];

  constructor(
    private readonly fileSystem: PspSaveFileSystem,
    private readonly manager: PspSaveManager,
    private readonly upload: (bytes: Uint8Array, event: SyncEvent) => Promise<boolean>,
    options: SyncOptions = {},
  ) {
    this.intervalMs = options.intervalMs ?? 3_000;
    this.stableMs = options.stableMs ?? 2_000;
    this.isPaused = options.isPaused ?? (() => false);
    this.onError = options.onError ?? (() => undefined);
    this.restartOnExit = options.restartOnExit ?? false;
    this.captureRoot = options.captureRoot ?? PERSISTENT_SAVE_ROOT;
    this.excludedBundlePaths = options.excludedBundlePaths ?? [];
    this.saved = fingerprintPspSaveTree(fileSystem, this.captureRoot, this.excludedBundlePaths);
  }

  start() {
    if (this.timer !== null) return;
    this.timer = setInterval(() => { void this.poll(); }, this.intervalMs);
  }

  stop() {
    if (this.timer !== null) clearInterval(this.timer);
    this.timer = null;
  }

  async poll(now = Date.now()) {
    if (this.active) return this.active;
    let current: PspSaveTreeFingerprint;
    try {
      this.manager.saveSaveFiles?.();
      current = fingerprintPspSaveTree(this.fileSystem, this.captureRoot, this.excludedBundlePaths);
    } catch (error) {
      this.report(error);
      return false;
    }
    if (current.value === this.saved.value) {
      this.candidate = null;
      this.failedFingerprint = null;
      return false;
    }
    if (!this.candidate || this.candidate.fingerprint.value !== current.value) {
      this.candidate = { fingerprint: current, observedAt: now };
      return false;
    }
    if (now - this.candidate.observedAt < this.stableMs) return false;
    return this.captureAndUpload(current, "AUTO_INTERVAL");
  }

  async flush() {
    this.stop();
    if (this.active) await this.active;
    let current: PspSaveTreeFingerprint;
    try {
      if (this.restartOnExit) this.manager.functions?.restart?.();
      this.manager.saveSaveFiles?.();
      current = fingerprintPspSaveTree(this.fileSystem, this.captureRoot, this.excludedBundlePaths);
    } catch (error) {
      this.report(error);
      return false;
    }
    if (current.fileCount === 0 && this.saved.fileCount === 0) return false;
    return this.captureAndUpload(current, "EXIT");
  }

  private captureAndUpload(fingerprint: PspSaveTreeFingerprint, event: SyncEvent) {
    if (this.active) return this.active;
    this.active = (async () => {
      try {
        if (!this.manager.toggleMainLoop) throw normalizedError();
        const shouldResume = !this.isPaused();
        this.manager.toggleMainLoop(false);
        let bytes: Uint8Array;
        try {
          bytes = snapshotPspSaveTree(this.fileSystem, this.captureRoot, this.excludedBundlePaths);
        } finally {
          this.manager.toggleMainLoop(shouldResume);
        }
        const uploaded = await this.upload(bytes, event);
        if (uploaded) {
          this.saved = fingerprint;
          this.candidate = null;
          this.failedFingerprint = null;
        }
        return uploaded;
      } catch (error) {
        this.report(error, fingerprint.value);
        return false;
      } finally {
        this.active = null;
      }
    })();
    return this.active;
  }

  private report(error: unknown, fingerprint?: string) {
    if (fingerprint && fingerprint === this.failedFingerprint) return;
    if (fingerprint) this.failedFingerprint = fingerprint;
    this.onError(error instanceof Error ? error : normalizedError());
  }
}

export const fingerprintPersistentSaveTree = fingerprintPspSaveTree;
export const restorePersistentSaveTree = restorePspSaveTree;
export const snapshotPersistentSaveTree = snapshotPspSaveTree;
export { PspPersistentSaveSync as PersistentSaveTreeSync };
