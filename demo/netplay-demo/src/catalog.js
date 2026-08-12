export const RUNTIME_VERSION = "4.2.3";

export const gameCatalog = Object.freeze({
  nes: Object.freeze({
    id: "nes-smash-ping-pong",
    label: "NES/FDS · Smash Ping Pong",
    core: "fceumm",
    system: "nes",
    gameId: 4101,
    gameUrl: "./assets/roms/nes-smash-ping-pong.zip",
    biosUrl: "./assets/roms/disksys.rom",
    gameSha256: "7e036a8df5bb73b71d0af8a4bca2904bf154e1e3b95faed70d526c27bd21440f",
    biosSha256: "99c18490ed9002d9c6d999b9d8d15be5c051bdfa7cc7e73318053c9a994b0178",
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
