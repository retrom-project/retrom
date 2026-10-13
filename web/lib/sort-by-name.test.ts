import { expect, it } from "vitest";
import { sortByName } from "./sort-by-name";

it("interleaves Chinese pinyin with Latin names and sorts numeric suffixes naturally", () => {
  const names = ["NES", "步步高 RPG", "Arcade", "世嘉", "Dreamcast", "任天堂", "Bandai", "Atari 7800", "Atari 5200", "Atari 2600"];
  const items = names.map((name, id) => ({ id: String(id), name }));
  expect(sortByName(items).map(item => item.name)).toEqual([
    "Arcade", "Atari 2600", "Atari 5200", "Atari 7800", "Bandai", "步步高 RPG", "Dreamcast", "NES", "任天堂", "世嘉",
  ]);
  expect(items.map(item => item.name)).toEqual(names);
});

it("normalizes full-width/case variants and gives equal names a stable ID tie-break", () => {
  const items = [{ id: "b", name: "NES" }, { id: "a", name: "NES" }, { id: "c", name: "arcade" }, { id: "d", name: "ＢＢＫ" }];
  expect(sortByName(items).map(item => item.id)).toEqual(["c", "d", "a", "b"]);
});
