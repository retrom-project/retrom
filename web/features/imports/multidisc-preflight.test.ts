import { describe, expect, it } from "vitest";
import { preflightMultiDisc } from "./multidisc-preflight";

function entry(path: string, contents: string) {
  return { path, file: new File([contents], path.split("/").at(-1) ?? path) };
}

describe("preflightMultiDisc", () => {
  it("keeps M3U order while reporting missing and ignored files", async () => {
    const result = await preflightMultiDisc([
      entry("saturn/game.m3u", "Disc B.CHD\ndisc-a.chd\nmissing.chd\n"),
      entry("saturn/disc-a.chd", "a"),
      entry("saturn/disc b.chd", "b"),
      entry("saturn/notes.txt", "ignored"),
    ]);

    expect(result).toMatchObject({
      detected: true,
      completeGroupCount: 0,
      blockedGroupCount: 1,
      rejectedGroupCount: 0,
      missingDiscCount: 1,
      ignoredFileCount: 1,
    });
    expect(result.groups[0]).toMatchObject({
      playlist: "game.m3u",
      discCount: 3,
      missing: ["missing.chd"],
      state: "BLOCKED",
    });
  });

  it("rejects unsafe, duplicate, oversized and ambiguous playlists", async () => {
    const unsafe = await preflightMultiDisc([entry("unsafe/game.m3u", "../one.chd\ntwo.chd\n")]);
    const duplicate = await preflightMultiDisc([entry("dupe/game.m3u", "ONE.CHD\none.chd\n")]);
    const oversized = await preflightMultiDisc([entry("large/game.m3u", "x".repeat(65_537))]);
    const ambiguous = await preflightMultiDisc([
      entry("pair/one.m3u", "one.chd\ntwo.chd\n"),
      entry("pair/two.m3u", "one.chd\ntwo.chd\n"),
    ]);

    for (const result of [unsafe, duplicate, oversized, ambiguous]) {
      expect(result.rejectedGroupCount).toBe(1);
      expect(result.completeGroupCount).toBe(0);
      expect(result.blockedGroupCount).toBe(0);
    }
  });

  it("does not classify an ordinary upload as multi-disc", async () => {
    await expect(preflightMultiDisc([entry("game.zip", "rom")])).resolves.toMatchObject({
      detected: false,
      groups: [],
      ignoredFileCount: 0,
    });
  });
});
