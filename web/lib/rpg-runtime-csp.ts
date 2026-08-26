const launchPath = /^\/play\/([0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/;

export function playerFrameSource(pathname: string, template: string | undefined) {
  const match = launchPath.exec(pathname);
  if (!match || !template || template.split("{launchId}").length !== 2) {return "'self'";}
  const launchId = match[1];
  const raw = template.replace("{launchId}", launchId);
  try {
    const parsed = new URL(raw);
    if ((parsed.protocol !== "https:" && parsed.protocol !== "http:") || parsed.username || parsed.password ||
      parsed.pathname !== "/" || parsed.search || parsed.hash || parsed.hostname.split(".")[0] !== launchId) {
      return "'self'";
    }
    return `'self' ${parsed.origin}`;
  } catch {
    return "'self'";
  }
}
