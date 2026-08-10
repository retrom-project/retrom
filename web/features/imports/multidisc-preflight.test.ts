import { describe, expect, it } from "vitest";
import { preflightMultiDisc } from "./multidisc-preflight";

function entry(path: string, contents: BlobPart) {
  return { path, file: new File([contents], path.split("/").at(-1) ?? path) };
}

function chd(path: string, payload = "disc") {
  return entry(path, `MComprHD${payload}`);
}

describe("preflightMultiDisc", () => {
  it("keeps M3U order while reporting canonical names, missing and ignored files", async () => {
    const result = await preflightMultiDisc([
      entry("saturn/game.m3u", "Disc B.CHD\ndisc-a.chd\nmissing.chd\n"),
      chd("saturn/disc-a.chd", "a"),
      chd("saturn/disc b.chd", "b"),
      entry("saturn/notes.txt", "ignored"),
      entry("unassociated/readme.txt", "ignored elsewhere"),
    ]);

    expect(result).toMatchObject({
      detected: true,
      completeGroupCount: 0,
      blockedGroupCount: 1,
      rejectedGroupCount: 0,
      processableGroupCount: 1,
      missingDiscCount: 1,
      ignoredFileCount: 2,
      unassociatedFiles: ["unassociated/readme.txt"],
    });
    expect(result.groups[0]).toMatchObject({
      playlist: "game.m3u",
      discCount: 3,
      presentDiscCount: 2,
      missing: ["missing.chd"],
      ignored: ["notes.txt"],
      state: "BLOCKED",
      entries: [
        { discIndex: 0, sourceReference: "Disc B.CHD", sourceBasename: "disc b.chd", canonicalName: "disc-001.chd", state: "PRESENT" },
        { discIndex: 1, sourceReference: "disc-a.chd", sourceBasename: "disc-a.chd", canonicalName: "disc-002.chd", state: "PRESENT" },
        { discIndex: 2, sourceReference: "missing.chd", sourceBasename: null, canonicalName: "disc-003.chd", state: "MISSING" },
      ],
    });
  });

  it("sorts recursive groups with the root first and keeps local ignore counts", async () => {
    const result = await preflightMultiDisc([
      entry("z/game.m3u", "z1.chd\nz2.chd\n"), chd("z/z1.chd"), chd("z/z2.chd"),
      entry("game.m3u", "root1.chd\nroot2.chd\n"), chd("root1.chd"), chd("root2.chd"), entry("note.txt", "x"),
      entry("a/game.m3u", "a1.chd\na2.chd\n"), chd("a/a1.chd"),
    ]);

    expect(result.groups.map((group) => group.directory)).toEqual([".", "a", "z"]);
    expect(result.groups.map((group) => group.state)).toEqual(["COMPLETE", "BLOCKED", "COMPLETE"]);
    expect(result.groups[0].ignored).toEqual(["note.txt"]);
    expect(result.completeGroupCount).toBe(2);
    expect(result.blockedGroupCount).toBe(1);
  });

  it("rejects unsafe, duplicate, invalid UTF-8, oversized and ambiguous playlists", async () => {
    const unsafe = await preflightMultiDisc([entry("unsafe/game.m3u", "../one.chd\ntwo.chd\n")]);
    const duplicate = await preflightMultiDisc([entry("dupe/game.m3u", "ONE.CHD\none.chd\n")]);
    const invalidUTF8 = await preflightMultiDisc([entry("utf8/game.m3u", new Uint8Array([0xff, 0x0a, 0x61]))]);
    const oversized = await preflightMultiDisc([entry("large/game.m3u", "x".repeat(65_537))]);
    const ambiguous = await preflightMultiDisc([
      entry("pair/one.m3u", "one.chd\ntwo.chd\n"),
      entry("pair/two.m3u", "one.chd\ntwo.chd\n"),
    ]);

    expect(unsafe.groups[0]).toMatchObject({ state: "REJECTED", reasonCode: "MULTI_DISC_REFERENCE_UNSAFE" });
    expect(duplicate.groups[0]).toMatchObject({ state: "REJECTED", reasonCode: "MULTI_DISC_PLAYLIST_INVALID" });
    expect(invalidUTF8.groups[0]).toMatchObject({ state: "REJECTED", reasonCode: "MULTI_DISC_PLAYLIST_INVALID" });
    expect(oversized.groups[0]).toMatchObject({ state: "REJECTED", reasonCode: "MULTI_DISC_LIMIT_EXCEEDED" });
    expect(ambiguous.groups[0]).toMatchObject({ state: "REJECTED", reasonCode: "MULTI_DISC_PLAYLIST_AMBIGUOUS" });
  });

  it("matches exact names before ASCII-fold fallback and rejects ambiguous fallback", async () => {
    const exact = await preflightMultiDisc([
      entry("exact/game.m3u", "Exact.chd\ntwo.chd\n"),
      chd("exact/Exact.chd"), chd("exact/exact.chd"), chd("exact/two.chd"),
    ]);
    const ambiguous = await preflightMultiDisc([
      entry("ambiguous/game.m3u", "EXACT.CHD\ntwo.chd\n"),
      chd("ambiguous/Exact.chd"), chd("ambiguous/exact.chd"), chd("ambiguous/two.chd"),
    ]);

    expect(exact.groups[0].state).toBe("COMPLETE");
    expect(exact.groups[0].entries[0]).toMatchObject({ sourceBasename: "Exact.chd" });
    expect(ambiguous.groups[0]).toMatchObject({ state: "REJECTED", reasonCode: "MULTI_DISC_PLAYLIST_INVALID" });
  });

  it("rejects invalid CHD headers and the selected platform limits", async () => {
    const invalid = await preflightMultiDisc([
      entry("bad/game.m3u", "one.chd\ntwo.chd\n"),
      entry("bad/one.chd", "not-a-chd"), chd("bad/two.chd"),
    ]);
    const tooLarge = await preflightMultiDisc([
      entry("large/game.m3u", "one.chd\ntwo.chd\n"), chd("large/one.chd"), chd("large/two.chd"),
    ], { maxDiscs: 8, maxTotalBytes: 16 });
    const tooMany = await preflightMultiDisc([
      entry("many/game.m3u", "one.chd\ntwo.chd\nthree.chd\n"),
    ], { maxDiscs: 2, maxTotalBytes: 1024 });

    expect(invalid.groups[0]).toMatchObject({ state: "REJECTED", reasonCode: "MULTI_DISC_CHD_INVALID" });
    expect(tooLarge.groups[0]).toMatchObject({ state: "REJECTED", reasonCode: "MULTI_DISC_LIMIT_EXCEEDED" });
    expect(tooMany.groups[0]).toMatchObject({ state: "REJECTED", reasonCode: "MULTI_DISC_LIMIT_EXCEEDED" });
  });

  it("does not classify an ordinary upload as multi-disc", async () => {
    await expect(preflightMultiDisc([entry("game.zip", "rom")])).resolves.toMatchObject({
      detected: false,
      groups: [],
      ignoredFileCount: 0,
    });
  });
});
