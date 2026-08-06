export function allowedDevOriginsFromPublicOrigin(raw: string | undefined): string[] | undefined {
  if (!raw) return undefined;

  let origin: URL;
  try {
    origin = new URL(raw);
  } catch {
    throw new Error("RETROM_PUBLIC_ORIGIN must be an absolute HTTP(S) origin");
  }
  if (
    (origin.protocol !== "http:" && origin.protocol !== "https:") ||
    !origin.hostname ||
    origin.username ||
    origin.password ||
    origin.pathname !== "/" ||
    origin.search ||
    origin.hash
  ) {
    throw new Error("RETROM_PUBLIC_ORIGIN must be an absolute HTTP(S) origin");
  }
  return [origin.hostname.toLowerCase()];
}
