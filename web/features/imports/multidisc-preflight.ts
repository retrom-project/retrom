export type PreflightFile = { path: string; file: File };

export type MultiDiscGroupPreview = {
  directory: string;
  playlist: string;
  discCount: number;
  missing: string[];
  state: "COMPLETE" | "BLOCKED" | "REJECTED";
  reason?: string;
};

export type MultiDiscPreflight = {
  detected: boolean;
  groups: MultiDiscGroupPreview[];
  completeGroupCount: number;
  blockedGroupCount: number;
  rejectedGroupCount: number;
  missingDiscCount: number;
  ignoredFileCount: number;
};

function dirname(value: string) {
  const index = value.lastIndexOf("/");
  return index < 0 ? "." : value.slice(0, index) || ".";
}

function basename(value: string) {
  const index = value.lastIndexOf("/");
  return index < 0 ? value : value.slice(index + 1);
}

function fold(value: string) {
  return value.replace(/[A-Z]/g, (character) => character.toLowerCase());
}

function safeReference(value: string) {
  return value.length >= 1 && value.length <= 255 && value.trim() === value &&
    !/[\x00-\x1f\x7f\\/?#]/.test(value) && value !== "." && value !== ".." &&
    !/^[A-Za-z][A-Za-z0-9+.-]*:/.test(value) && value.toLowerCase().endsWith(".chd");
}

async function inspectPlaylist(file: File) {
  if (file.size > 65_536) return { references: [] as string[], reason: "M3U 超过 64 KiB" };
  const raw = await file.text();
  const text = raw.charCodeAt(0) === 0xfeff ? raw.slice(1) : raw;
  const references: string[] = [];
  const seen = new Set<string>();
  for (const line of text.split("\n")) {
    const value = line.endsWith("\r") ? line.slice(0, -1) : line;
    if (!value || value.startsWith("#")) continue;
    if (!safeReference(value) || seen.has(fold(value))) return { references: [], reason: "M3U 引用不安全、重复或不是 CHD" };
    seen.add(fold(value));
    references.push(value);
  }
  if (references.length < 2 || references.length > 8) return { references: [], reason: "每组必须包含 2–8 张光盘" };
  return { references, reason: "" };
}

export async function preflightMultiDisc(files: PreflightFile[]): Promise<MultiDiscPreflight> {
  const playlists = files.filter((entry) => entry.path.toLowerCase().endsWith(".m3u"));
  if (playlists.length === 0) return {
    detected: false, groups: [], completeGroupCount: 0, blockedGroupCount: 0,
    rejectedGroupCount: 0, missingDiscCount: 0, ignoredFileCount: 0,
  };
  const byDirectory = new Map<string, PreflightFile[]>();
  for (const playlist of playlists) {
    const directory = dirname(playlist.path);
    byDirectory.set(directory, [...(byDirectory.get(directory) ?? []), playlist]);
  }
  const referencedPaths = new Set<string>();
  const groups: MultiDiscGroupPreview[] = [];
  for (const directory of [...byDirectory.keys()].sort()) {
    const directoryPlaylists = byDirectory.get(directory) ?? [];
    if (directoryPlaylists.length !== 1) {
      groups.push({ directory, playlist: "", discCount: 0, missing: [], state: "REJECTED", reason: "同一目录只能有一个 M3U" });
      continue;
    }
    const playlist = directoryPlaylists[0];
    referencedPaths.add(playlist.path);
    const parsed = await inspectPlaylist(playlist.file);
    if (parsed.reason) {
      groups.push({ directory, playlist: basename(playlist.path), discCount: 0, missing: [], state: "REJECTED", reason: parsed.reason });
      continue;
    }
    const candidates = files.filter((entry) => dirname(entry.path) === directory);
    const folded = new Map(candidates.map((entry) => [fold(basename(entry.path)), entry]));
    const missing: string[] = [];
    for (const reference of parsed.references) {
      const matched = folded.get(fold(reference));
      if (matched) referencedPaths.add(matched.path); else missing.push(reference);
    }
    groups.push({
      directory, playlist: basename(playlist.path), discCount: parsed.references.length, missing,
      state: missing.length ? "BLOCKED" : "COMPLETE",
    });
  }
  return {
    detected: true,
    groups,
    completeGroupCount: groups.filter((group) => group.state === "COMPLETE").length,
    blockedGroupCount: groups.filter((group) => group.state === "BLOCKED").length,
    rejectedGroupCount: groups.filter((group) => group.state === "REJECTED").length,
    missingDiscCount: groups.reduce((total, group) => total + group.missing.length, 0),
    ignoredFileCount: files.filter((entry) => !referencedPaths.has(entry.path)).length,
  };
}
