export const RUNTIME_VERSION = "4.2.3";

export const gameCatalog = Object.freeze({
  nes: Object.freeze({
    id: "nes-f1-race",
    label: "NES · F-1 Race",
    core: "fceumm",
    system: "nes",
    gameId: 4101,
    gameUrl: "./assets/roms/nes-f1-race.zip",
    gameSha256: "aa9a4e5959851440c507aaa551a66eab6fe8623179a8086cb2ec8606cb830393",
    coreSha256: "8c449fd5c36646fb0769423ed6ffa9efbdfc21fbfdc9bac7952b559d34d5b493"
  }),
  fbneo: Object.freeze({
    id: "fbneo-ldrun",
    label: "FBNeo · Lode Runner",
    core: "fbneo",
    system: "arcade",
    gameId: 4102,
    gameUrl: "./assets/roms/ldrun.zip",
    gameSha256: "b45507a74f739e27a5486d79901016b78e061c4db2025435d4df37702553e8d9",
    coreSha256: "315a25e0bcd61d58ee0d9e8b1dbf3740b9e0ca4b7d0726f848ce1068de73437c"
  })
});

export function getGameConfig(core) {
  const config = gameCatalog[core];
  if (!config) throw new Error(`Unsupported demo core: ${core}`);
  return config;
}
