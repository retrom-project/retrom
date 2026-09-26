const platformArtwork: Readonly<Record<string, string>> = {
  snes: "/images/platforms/snes.svg",
  gba: "/images/platforms/gba.svg",
  psx: "/images/platforms/psx.svg",
  megadrive: "/images/platforms/megadrive.svg",
};

export function platformArt(id: string): string | null {
  return Object.hasOwn(platformArtwork, id) ? platformArtwork[id] : null;
}
