import { describe, expect, it } from "vitest";
import type { DiscSet, EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import { prepareMultiDiscLaunch } from "./multi-disc-restore";

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
  const instance: EmulatorInstance = {
    on: () => undefined,
    gameManager: {
      getDiskCount: () => 3,
      getCurrentDisk: () => currentDisc,
      setCurrentDisk: (index) => { calls.push(`disc:${index}`); currentDisc = index; },
      toggleMainLoop: (running) => { calls.push(`loop:${running}`); },
    },
  };
  return { calls, instance };
}

describe("prepareMultiDiscLaunch", () => {
  it("selects the saved disc and leaves the loop paused for the explicit restore boundary", () => {
    const { calls, instance } = fixture();
    const selected = prepareMultiDiscLaunch(instance, discSet);
    expect(selected).toEqual({ count: 3, currentIndex: 1 });
    expect(calls).toEqual(["loop:false", "disc:1"]);
  });

  it("keeps the main loop stopped when disc selection fails", () => {
    const { calls, instance } = fixture();
    if (instance.gameManager) instance.gameManager.getDiskCount = () => 2;
    expect(() => prepareMultiDiscLaunch(instance, discSet)).toThrow("PLAYER_DISC_SET_INVALID");
    expect(calls).toEqual(["loop:false"]);
  });
});
