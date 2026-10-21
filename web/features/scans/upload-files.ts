import type { UploadEntry } from "./upload-transfer";

/** Files from a file or folder input; a folder keeps its own name as the top-level path. */
export function inputEntries(files: FileList | null): UploadEntry[] {
  return Array.from(files ?? [], (file) => ({ path: file.webkitRelativePath || file.name, file }));
}

/** Files dropped onto the page, walking dropped folders the way a folder input would. */
export async function droppedEntries(transfer: DataTransfer): Promise<UploadEntry[]> {
  const roots = Array.from(transfer.items, (item) => item.webkitGetAsEntry()).filter(
    (entry): entry is FileSystemEntry => entry !== null,
  );
  if (!roots.length) { return inputEntries(transfer.files); }
  const result: UploadEntry[] = [];
  for (const root of roots) { result.push(...await walk(root)); }
  return result;
}

async function walk(entry: FileSystemEntry): Promise<UploadEntry[]> {
  const path = entry.fullPath.replace(/^\/+/u, "");
  if (entry.isFile) {
    const file = await new Promise<File>((resolve, reject) => (entry as FileSystemFileEntry).file(resolve, reject));
    return [{ path, file }];
  }
  const reader = (entry as FileSystemDirectoryEntry).createReader();
  const result: UploadEntry[] = [];
  // readEntries returns the children in batches until it yields an empty batch.
  for (let batch = await readBatch(reader); batch.length; batch = await readBatch(reader)) {
    for (const child of batch) { result.push(...await walk(child)); }
  }
  return result;
}

function readBatch(reader: FileSystemDirectoryReader) {
  return new Promise<FileSystemEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
}

/** Adds a new selection; a path chosen again is replaced by its latest file. */
export function mergeEntries(current: UploadEntry[], added: UploadEntry[]): UploadEntry[] {
  const byPath = new Map(current.map((entry) => [entry.path, entry]));
  for (const entry of added) { byPath.set(entry.path, entry); }
  return [...byPath.values()].sort((left, right) => left.path.localeCompare(right.path));
}

const coverPattern = /\.(png|jpe?g|webp)$/iu;

/** The folder every selected path sits in, when the selection is exactly one top-level folder. */
export function singleFolder(entries: UploadEntry[]): string {
  const first = entries[0]?.path.split("/")[0] ?? "";
  return first && entries.every((entry) => entry.path.startsWith(`${first}/`)) ? first : "";
}

/** Drops the shared top-level folder so each file inside it becomes its own loose game. */
export function unwrapFolder(entries: UploadEntry[], folder: string): UploadEntry[] {
  return entries.map((entry) => ({ ...entry, path: entry.path.slice(folder.length + 1) }));
}

/** A folder of loose ROMs (files only, no subfolders) rather than one multi-file game. */
export function looksLikeLooseFolder(entries: UploadEntry[], folder: string) {
  const inner = unwrapFolder(entries, folder);
  return inner.length > 0 && inner.every((entry) => !entry.path.includes("/"));
}

/** Game titles a loose source would produce, mirroring the server's top-level rule. */
export function looseTitles(entries: UploadEntry[]): string[] {
  const names = [...new Set(entries.map((entry) => entry.path.split("/")[0]))].filter((name) => !name.startsWith("."));
  const stem = (name: string) => name.replace(/\.[^.]+$/u, "") || name;
  const games = names.filter((name) => !coverPattern.test(name) || names.every((other) =>
    other === name || stem(other) !== stem(name) || coverPattern.test(other)));
  return games.map((name) => (entries.some((entry) => entry.path.startsWith(`${name}/`)) ? name : stem(name)))
    .sort((left, right) => left.localeCompare(right));
}
