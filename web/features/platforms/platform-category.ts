export type DirectoryCategory = "arcade" | "console" | "handheld" | "computer" | "runtime" | "other";

export const directoryCategories: ReadonlyArray<{ id: DirectoryCategory; label: string }> = [
  { id: "arcade", label: "街机" },
  { id: "console", label: "家用主机" },
  { id: "handheld", label: "掌机" },
  { id: "computer", label: "电脑" },
  { id: "runtime", label: "游戏引擎与运行环境" },
  { id: "other", label: "其他" },
];

// A directory inherits its category from its base platform, including custom directories.
const platformCategory: Record<string, DirectoryCategory> = {
  "3do": "console", amiga: "computer", amigacd32: "console", amstradcpc: "computer", arcade: "arcade", arduboy: "handheld",
  atari2600: "console", atari5200: "console", atari7800: "console", atari800: "computer",
  atarijaguar: "console", atarist: "computer", bbc: "computer", bbkrpg: "computer",
  butterscotch: "runtime", c128: "computer", c64: "computer", cavestory: "runtime",
  cdi: "console", channelf: "console", colecovision: "console", doom: "runtime",
  dos: "computer", dreamcast: "console", fds: "console", flash: "runtime",
  gamegear: "handheld", gba: "handheld", gbc: "handheld", gx4000: "console",
  intellivision: "console", j2me: "runtime", kirikiri: "runtime", lynx: "handheld",
  mastersystem: "console", megadrive: "console", megaduck: "handheld", model3: "arcade",
  msx: "computer", multivision: "console", n64: "console", nds: "handheld", neogeocd: "console", nes: "console",
  ngpc: "handheld", nintendo3ds: "handheld", odyssey2: "console", ons: "runtime", openbor: "runtime",
  pc88: "computer", pc98: "computer", pce: "console", pcecd: "console",
  pcfx: "console", pet: "computer", pico: "console", pico8: "runtime",
  plus4: "computer", pokemini: "handheld", ps2: "console", psp: "handheld",
  psx: "console", rpgmaker: "runtime", samcoupe: "computer", satellaview: "console", saturn: "console", scummvm: "runtime",
  sega32x: "console", segacd: "console", sg1000: "console", snes: "console", supergrafx: "console",
  supervision: "handheld", thomson: "computer", tic80: "runtime", tyranoscript: "runtime",
  uzebox: "console", vectrex: "console",
  vic20: "computer", virtualboy: "console", wasm4: "runtime", wonderswan: "handheld",
  x68000: "computer", xegs: "console", zx81: "computer", zxspectrum: "computer",
};

export function categoryForPlatform(platformId: string | undefined): DirectoryCategory {
  return platformId ? platformCategory[platformId] ?? "other" : "other";
}

export function categoryLabelForPlatform(platformId: string | undefined): string {
  return directoryCategories.find((category) => category.id === categoryForPlatform(platformId))?.label ?? "其他";
}
