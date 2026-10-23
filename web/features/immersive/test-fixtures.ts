import type { Save } from "@/lib/api/types";

export function saveFixture(id: string, gameId = "game"): Save {
  return {
    id, name: `进度 ${id}`, kind: "checkpoint", slot: null, version: 1,
    sizeBytes: 4706100, createdAtMs: 0, updatedAtMs: 0, screenshotUrl: null,
    restorable: true, restoreReason: "", restoreWarning: "",
    game: {
      id: gameId, platformInstanceId: "directory", platformId: "nes", directoryName: "NES",
      title: `游戏 ${gameId}`, titleLetter: "#", sortName: "", description: "", developer: "", publisher: "", genre: "", players: null,
      releaseYear: null, status: "published", source: "server_import", version: 1,
      contentHash: "a".repeat(64), tags: [], media: [], favorite: false, lastPlayedAtMs: null, createdAtMs: 0, updatedAtMs: 0,
    },
    extinfo: {
      coreId: "fceumm", providerId: "emulatorjs", targetId: "fceumm",
      coreFingerprint: "b".repeat(64), romHash: "a".repeat(64), checkpointFormat: "state-storage-v1",
      content: { kind: "SINGLE_FILE", entryFile: "game.nes" }, runtimeOptions: {},
    },
  };
}
