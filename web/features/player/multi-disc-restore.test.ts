import { describe, expect, it, vi } from "vitest";
import type { DiscSet, EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import { restoreMultiDiscLaunch } from "./multi-disc-restore";

const discSet: DiscSet = {
  contentKind: "MULTI_DISC_M3U_V1",
  count: 3,
  initialDiscIndex: 1,
  entries: [
    { index: 0, label: "光盘 1", virtualPath: "/disc-001.chd" },
    { index: 1, label: "光盘 2", virtualPath: "/disc-002.chd" },
    { index: 2, label: "光盘 3", virtualPath: "/disc-003.chd" },
  ],
};

function fixture() {
  const calls: string[] = [];
  let currentDisc = 0;
  const fileSystem = {
    analyzePath: vi.fn((path: string) => ({ exists: ["/data", "/data/saves"].includes(path) })),
    mkdir: vi.fn((path: string) => { calls.push(`mkdir:${path}`); }),
    writeFile: vi.fn((path: string) => { calls.push(`write:${path}`); }),
    unlink: vi.fn((path: string) => { calls.push(`unlink:${path}`); }),
  };
  const instance: EmulatorInstance = {
    on: () => undefined,
    gameManager: {
      FS: fileSystem,
      getDiskCount: () => 3,
      getCurrentDisk: () => currentDisc,
      setCurrentDisk: (index) => { calls.push(`disc:${index}`); currentDisc = index; },
      loadSaveFiles: () => { calls.push("persistent:load"); },
      loadState: () => { calls.push("state:load"); },
      toggleMainLoop: (running) => { calls.push(`loop:${running}`); },
    },
  };
  return { calls, fileSystem, instance };
}

describe("restoreMultiDiscLaunch", () => {
  it("restores persistent bytes, selects the saved disc, and loads state before resuming", () => {
    const { calls, fileSystem, instance } = fixture();
    const selected = restoreMultiDiscLaunch(instance, discSet, {
      fileSystem,
      savePath: "/data/saves/game.srm",
      bytes: Uint8Array.of(1, 2, 3),
    }, Uint8Array.of(4, 5, 6));
    expect(selected).toEqual({ count: 3, currentIndex: 1 });
    expect(calls).toEqual([
      "loop:false",
      "write:/data/saves/game.srm",
      "persistent:load",
      "disc:1",
      "state:load",
      "loop:true",
    ]);
  });

  it("keeps the main loop stopped when a required restore boundary fails", () => {
    const { calls, instance } = fixture();
    if (instance.gameManager) instance.gameManager.loadState = undefined;
    expect(() => restoreMultiDiscLaunch(instance, discSet, null, Uint8Array.of(1)))
      .toThrow("PLAYER_SAVE_STATE_UNAVAILABLE");
    expect(calls).toEqual(["loop:false", "disc:1"]);
  });
});
