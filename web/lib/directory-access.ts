const MAX_DIRECTORY_FILES = 10_000;

type ReadableFileHandle = Pick<FileSystemFileHandle, "getFile" | "kind" | "name">;
type ReadableDirectoryHandle = Pick<FileSystemDirectoryHandle, "kind" | "name"> & {
  entries: () => AsyncIterableIterator<[string, ReadableDirectoryHandle | ReadableFileHandle]>;
};

type DirectoryPickerWindow = Window & {
  showDirectoryPicker?: () => Promise<ReadableDirectoryHandle>;
};

export type PickedDirectoryFile = {
  file: File;
  relativePath: string;
};

export type PickedDirectory = {
  files: PickedDirectoryFile[];
  name: string;
};

export function directoryPickerAvailable() {
  return typeof window !== "undefined" && typeof (window as DirectoryPickerWindow).showDirectoryPicker === "function";
}

function compareEntryNames(
  [left]: [string, ReadableDirectoryHandle | ReadableFileHandle],
  [right]: [string, ReadableDirectoryHandle | ReadableFileHandle],
) {
  if (left === right) {return 0;}
  return left < right ? -1 : 1;
}

async function readEntries(
  directory: ReadableDirectoryHandle,
  path: string[],
  files: PickedDirectoryFile[],
) {
  const entries: Array<[string, ReadableDirectoryHandle | ReadableFileHandle]> = [];
  for await (const entry of directory.entries()) {entries.push(entry);}
  entries.sort(compareEntryNames);
  for (const [name, handle] of entries) {
    const relativePath = [...path, name];
    if (handle.kind === "directory") {
      await readEntries(handle, relativePath, files);
      continue;
    }
    files.push({ file: await handle.getFile(), relativePath: relativePath.join("/") });
    if (files.length > MAX_DIRECTORY_FILES) {
      throw new Error(`目录文件数不能超过 ${MAX_DIRECTORY_FILES.toLocaleString("en-US")} 个`);
    }
  }
}

export async function pickDirectory(): Promise<PickedDirectory | null> {
  const browser = window as DirectoryPickerWindow;
  if (!browser.showDirectoryPicker) {throw new Error("当前浏览器不支持目录读取，请使用项目支持的 Chrome");}
  try {
    const directory = await browser.showDirectoryPicker();
    const files: PickedDirectoryFile[] = [];
    await readEntries(directory, [directory.name], files);
    return { files, name: directory.name };
  } catch (error) {
    if (typeof error === "object" && error !== null && "name" in error && error.name === "AbortError") {return null;}
    throw error;
  }
}

export function droppedDirectory(files: File[]): PickedDirectory {
  const firstRelativePath = files.find((file) => file.webkitRelativePath)?.webkitRelativePath ?? "";
  return {
    files: files.map((file) => ({ file, relativePath: file.webkitRelativePath || file.name })),
    name: firstRelativePath.split("/")[0] || "所选目录",
  };
}
