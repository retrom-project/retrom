export function normalizedBase(value) {
  const parsed = new URL(value);
  if (parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== "/") {
    throw new Error("RPG_ACCEPTANCE_BASE_URL_INVALID");
  }
  const localHostname = isLocalAcceptanceHostname(parsed.hostname);
  if (parsed.protocol !== "https:" && (parsed.protocol !== "http:" || !localHostname)) {
    throw new Error("RPG_ACCEPTANCE_BASE_URL_REQUIRES_HTTPS");
  }
  return parsed.origin;
}

export function isLocalAcceptanceHostname(hostname) {
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname.endsWith(".localhost");
}
