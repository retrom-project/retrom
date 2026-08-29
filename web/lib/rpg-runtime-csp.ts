const exampleLaunchId = "0198abcd-1234-7123-8abc-1234567890ab";

export function playerFrameSource(template: string | undefined) {
  if (!template || template.split("{launchId}").length !== 2) {return "'self'";}
  const raw = template.replace("{launchId}", exampleLaunchId);
  try {
    const parsed = new URL(raw);
    if ((parsed.protocol !== "https:" && parsed.protocol !== "http:") || parsed.username || parsed.password ||
      parsed.pathname !== "/" || parsed.search || parsed.hash) {
      return "'self'";
    }
    const labels = parsed.hostname.split(".");
    if (labels.length < 2 || labels[0] !== exampleLaunchId || labels.slice(1).some((label) => !label)) {
      return "'self'";
    }
    const port = parsed.port ? `:${parsed.port}` : "";
    return `'self' ${parsed.protocol}//*.${labels.slice(1).join(".")}${port}`;
  } catch {
    return "'self'";
  }
}
