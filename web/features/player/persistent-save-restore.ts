export type PersistentSaveFileSystem = {
  analyzePath: (path: string) => { exists: boolean };
  mkdir: (path: string) => void;
  writeFile: (path: string, bytes: Uint8Array) => void;
  unlink: (path: string) => void;
};

export type PersistentSaveManager = {
  loadSaveFiles?: () => void;
  toggleMainLoop?: (running: boolean) => void;
};

function ensureDirectory(fileSystem: PersistentSaveFileSystem, filePath: string) {
  const segments = filePath.split("/").filter(Boolean);
  let current = "";
  for (const segment of segments.slice(0, -1)) {
    current += `/${segment}`;
    if (!fileSystem.analyzePath(current).exists) fileSystem.mkdir(current);
  }
}

export function restorePersistentSave(
  manager: PersistentSaveManager,
  fileSystem: PersistentSaveFileSystem,
  savePath: string,
  serverBytes: Uint8Array | null
) {
  try {
    if (!manager.toggleMainLoop) throw new Error("runtime main loop is unavailable");
    manager.toggleMainLoop(false);
    if (!manager.loadSaveFiles) throw new Error("runtime save reload is unavailable");
    ensureDirectory(fileSystem, savePath);
    if (serverBytes) fileSystem.writeFile(savePath, serverBytes);
    else if (fileSystem.analyzePath(savePath).exists) fileSystem.unlink(savePath);
    manager.loadSaveFiles();
    manager.toggleMainLoop(true);
  } catch {
    throw new Error("LAUNCH_PERSISTENT_SAVE_LOAD_FAILED");
  }
}
