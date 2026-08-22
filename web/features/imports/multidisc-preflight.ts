export const MULTI_DISC_DEFAULT_LIMITS = { maxDiscs: 8, maxTotalBytes: 1_073_741_824 } as const;

export type PreflightFile = { path: string; file: File };
export type MultiDiscPreflightLimits = { maxDiscs: number; maxTotalBytes: number };

export type MultiDiscEntryPreview = {
  discIndex: number;
  label: string;
  sourceReference: string;
  canonicalName: string;
  state: "PRESENT" | "MISSING";
  sourceBasename: string | null;
  sizeBytes: number | null;
};

export type MultiDiscGroupPreview = {
  directory: string;
  playlist: string;
  playlistSizeBytes: number;
  discCount: number;
  presentDiscCount: number;
  presentTotalBytes: number;
  entries: MultiDiscEntryPreview[];
  missing: string[];
  ignored: string[];
  ignoredCount: number;
  state: "COMPLETE" | "BLOCKED" | "REJECTED";
  reasonCode?: string;
  reason?: string;
};

export type MultiDiscPreflight = {
  detected: boolean;
  groups: MultiDiscGroupPreview[];
  completeGroupCount: number;
  blockedGroupCount: number;
  rejectedGroupCount: number;
  processableGroupCount: number;
  missingDiscCount: number;
  ignoredFileCount: number;
  unassociatedFiles: string[];
  limits: MultiDiscPreflightLimits;
};

type ParsedPlaylist = { references: string[]; reasonCode: string; reason: string };

const textEncoder = new TextEncoder();

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

function safeBasename(value: string) {
  const bytes = textEncoder.encode(value);
  return bytes.length >= 1 && bytes.length <= 255 &&
    !/[\x00-\x1f\x7f\\/?#]/.test(value) &&
    !/^[ \t\n\r\v\f]|[ \t\n\r\v\f]$/.test(value) &&
    value !== "." && value !== ".." &&
    !/^[A-Za-z][A-Za-z0-9+.-]*:/.test(value);
}

function rejected(
  directory: string,
  playlist: string,
  playlistSizeBytes: number,
  reasonCode: string,
  reason: string,
  ignored: string[],
): MultiDiscGroupPreview {
  return {
    directory,
    playlist,
    playlistSizeBytes,
    discCount: 0,
    presentDiscCount: 0,
    presentTotalBytes: 0,
    entries: [],
    missing: [],
    ignored,
    ignoredCount: ignored.length,
    state: "REJECTED",
    reasonCode,
    reason,
  };
}

function validateLimits(limits: MultiDiscPreflightLimits) {
  return Number.isSafeInteger(limits.maxDiscs) && limits.maxDiscs >= 2 && limits.maxDiscs <= 8 &&
    Number.isSafeInteger(limits.maxTotalBytes) && limits.maxTotalBytes > 0;
}

async function inspectPlaylist(file: File, maxDiscs: number): Promise<ParsedPlaylist> {
  if (file.size > 65_536) {
    return { references: [], reasonCode: "MULTI_DISC_LIMIT_EXCEEDED", reason: "M3U 超过 64 KiB" };
  }
  const text = await readPlaylistText(file);
  if (text === null) {
    return { references: [], reasonCode: "MULTI_DISC_PLAYLIST_INVALID", reason: "M3U 无法读取或不是有效 UTF-8" };
  }
  const references: string[] = [];
  const seen = new Set<string>();
  for (const rawLine of text.split("\n")) {
    const value = rawLine.endsWith("\r") ? rawLine.slice(0, -1) : rawLine;
    if (!value || value.startsWith("#")) {continue;}
    if (!safeBasename(value)) {
      return { references: [], reasonCode: "MULTI_DISC_REFERENCE_UNSAFE", reason: "M3U 引用必须是安全的同目录文件名" };
    }
    if (!fold(value).endsWith(".chd")) {
      return { references: [], reasonCode: "MULTI_DISC_PLAYLIST_INVALID", reason: "M3U 只能引用 CHD 文件" };
    }
    const normalized = fold(value);
    if (seen.has(normalized)) {
      return { references: [], reasonCode: "MULTI_DISC_PLAYLIST_INVALID", reason: "M3U 包含重复光盘引用" };
    }
    seen.add(normalized);
    references.push(value);
    if (references.length > maxDiscs) {
      return { references: [], reasonCode: "MULTI_DISC_LIMIT_EXCEEDED", reason: `每组最多 ${maxDiscs} 张光盘` };
    }
  }
  if (references.length < 2) {
    return { references: [], reasonCode: "MULTI_DISC_PLAYLIST_INVALID", reason: "每组必须至少包含 2 张光盘" };
  }
  return { references, reasonCode: "", reason: "" };
}

async function readPlaylistText(file: File) {
  try {
    const bytes = new Uint8Array(await file.arrayBuffer());
    const body = bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf
      ? bytes.subarray(3)
      : bytes;
    return new TextDecoder("utf-8", { fatal: true }).decode(body);
  } catch {
    return null;
  }
}

async function hasCHDMagic(file: File) {
  if (file.size < 8) {return false;}
  try {
    const header = new Uint8Array(await file.slice(0, 8).arrayBuffer());
    return new TextDecoder("ascii").decode(header) === "MComprHD";
  } catch {
    return false;
  }
}

function groupFiles(files: PreflightFile[], directory: string) {
  return files.filter((entry) => dirname(entry.path) === directory);
}

async function inspectGroup(
  directory: string,
  playlists: PreflightFile[],
  files: PreflightFile[],
  limits: MultiDiscPreflightLimits,
  referencedPaths: Set<string>,
): Promise<MultiDiscGroupPreview> {
  for (const playlist of playlists) {referencedPaths.add(playlist.path);}
  const directoryFiles = groupFiles(files, directory);
  if (playlists.length !== 1) {
    const ignored = directoryFiles.filter((entry) => !referencedPaths.has(entry.path)).map((entry) => basename(entry.path));
    return rejected(directory, "", 0, "MULTI_DISC_PLAYLIST_AMBIGUOUS", "同一目录只能有一个 M3U", ignored);
  }
  const playlist = playlists[0];
  const parsed = await inspectPlaylist(playlist.file, limits.maxDiscs);
  if (parsed.reason) {
    const ignored = directoryFiles.filter((entry) => entry.path !== playlist.path).map((entry) => basename(entry.path));
    return rejected(directory, basename(playlist.path), playlist.file.size, parsed.reasonCode, parsed.reason, ignored);
  }

  const candidates = directoryFiles.filter((entry) => entry.path !== playlist.path);
  const inspected = await inspectDiscReferences(parsed.references, candidates, limits, referencedPaths);
  if (inspected.error) {
    const ignored = candidates.filter((entry) => !referencedPaths.has(entry.path)).map((entry) => basename(entry.path));
    return rejected(
      directory, basename(playlist.path), playlist.file.size, inspected.error.code, inspected.error.reason, ignored,
    );
  }
  const { entries, presentTotalBytes } = inspected;
  const missing = entries.filter((entry) => entry.state === "MISSING").map((entry) => entry.sourceReference);
  const ignored = candidates.filter((entry) => !referencedPaths.has(entry.path)).map((entry) => basename(entry.path));
  return {
    directory,
    playlist: basename(playlist.path),
    playlistSizeBytes: playlist.file.size,
    discCount: entries.length,
    presentDiscCount: entries.length - missing.length,
    presentTotalBytes,
    entries,
    missing,
    ignored,
    ignoredCount: ignored.length,
    state: missing.length ? "BLOCKED" : "COMPLETE",
  };
}

function indexDiscCandidates(candidates: PreflightFile[]) {
  const exact = new Map<string, PreflightFile[]>();
  const folded = new Map<string, PreflightFile[]>();
  for (const candidate of candidates) {
    const name = basename(candidate.path);
    exact.set(name, [...(exact.get(name) ?? []), candidate]);
    folded.set(fold(name), [...(folded.get(fold(name)) ?? []), candidate]);
  }
  return { exact, folded };
}

async function inspectDiscReferences(
  references: string[],
  candidates: PreflightFile[],
  limits: MultiDiscPreflightLimits,
  referencedPaths: Set<string>,
) {
  const { exact, folded } = indexDiscCandidates(candidates);
  const entries: MultiDiscEntryPreview[] = [];
  let presentTotalBytes = 0;
  for (const [discIndex, reference] of references.entries()) {
    const exactMatches = exact.get(reference) ?? [];
    const matches = exactMatches.length ? exactMatches : folded.get(fold(reference)) ?? [];
    if (matches.length > 1) {
      return { entries, presentTotalBytes, error: { code: "MULTI_DISC_PLAYLIST_INVALID", reason: `光盘引用 ${reference} 的大小写匹配不唯一` } };
    }
    const matched = matches[0];
    if (matched) {
      if (!await hasCHDMagic(matched.file)) {
        return { entries, presentTotalBytes, error: { code: "MULTI_DISC_CHD_INVALID", reason: `${reference} 不是有效 CHD` } };
      }
      if (matched.file.size > limits.maxTotalBytes - presentTotalBytes) {
        return { entries, presentTotalBytes, error: { code: "MULTI_DISC_LIMIT_EXCEEDED", reason: `光盘总大小超过 ${limits.maxTotalBytes} bytes` } };
      }
      referencedPaths.add(matched.path);
      presentTotalBytes += matched.file.size;
    }
    entries.push({
      discIndex,
      label: `光盘 ${discIndex + 1}`,
      sourceReference: reference,
      canonicalName: `disc-${String(discIndex + 1).padStart(3, "0")}.chd`,
      state: matched ? "PRESENT" : "MISSING",
      sourceBasename: matched ? basename(matched.path) : null,
      sizeBytes: matched?.file.size ?? null,
    });
  }
  return { entries, presentTotalBytes, error: null };
}

export async function preflightMultiDisc(
  files: PreflightFile[],
  limits: MultiDiscPreflightLimits = MULTI_DISC_DEFAULT_LIMITS,
): Promise<MultiDiscPreflight> {
  const normalizedLimits = { maxDiscs: limits.maxDiscs, maxTotalBytes: limits.maxTotalBytes };
  const playlists = files.filter((entry) => fold(entry.path).endsWith(".m3u"));
  const empty = {
    detected: false,
    groups: [],
    completeGroupCount: 0,
    blockedGroupCount: 0,
    rejectedGroupCount: 0,
    processableGroupCount: 0,
    missingDiscCount: 0,
    ignoredFileCount: 0,
    unassociatedFiles: [],
    limits: normalizedLimits,
  } satisfies MultiDiscPreflight;
  if (playlists.length === 0) {return empty;}

  const byDirectory = new Map<string, PreflightFile[]>();
  for (const playlist of playlists) {
    const directory = dirname(playlist.path);
    byDirectory.set(directory, [...(byDirectory.get(directory) ?? []), playlist]);
  }
  const referencedPaths = new Set<string>();
  const groups: MultiDiscGroupPreview[] = [];
  const directories = [...byDirectory.keys()].sort((left, right) => left === "." ? -1 : right === "." ? 1 : left.localeCompare(right));
  for (const directory of directories) {
    if (!validateLimits(normalizedLimits)) {
      const groupPlaylists = byDirectory.get(directory) ?? [];
      groups.push(rejected(directory, groupPlaylists.length === 1 ? basename(groupPlaylists[0].path) : "", groupPlaylists[0]?.file.size ?? 0, "MULTI_DISC_LIMIT_EXCEEDED", "平台返回的多盘限制无效", []));
      continue;
    }
    groups.push(await inspectGroup(directory, byDirectory.get(directory) ?? [], files, normalizedLimits, referencedPaths));
  }
  const groupedDirectories = new Set(directories);
  const unassociatedFiles = files
    .filter((entry) => !groupedDirectories.has(dirname(entry.path)))
    .map((entry) => entry.path)
    .sort((left, right) => left.localeCompare(right));
  const ignoredFileCount = groups.reduce((total, group) => total + group.ignoredCount, 0) + unassociatedFiles.length;
  const completeGroupCount = groups.filter((group) => group.state === "COMPLETE").length;
  const blockedGroupCount = groups.filter((group) => group.state === "BLOCKED").length;
  const rejectedGroupCount = groups.filter((group) => group.state === "REJECTED").length;
  return {
    detected: true,
    groups,
    completeGroupCount,
    blockedGroupCount,
    rejectedGroupCount,
    processableGroupCount: completeGroupCount + blockedGroupCount,
    missingDiscCount: groups.reduce((total, group) => total + group.missing.length, 0),
    ignoredFileCount,
    unassociatedFiles,
    limits: normalizedLimits,
  };
}
