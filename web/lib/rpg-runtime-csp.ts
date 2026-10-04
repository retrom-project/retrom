const exampleLaunchId = "0198abcd-1234-7123-8abc-1234567890ab";

function isOriginOnly(parsed: URL, raw: string) {
  return (parsed.protocol === "https:" || parsed.protocol === "http:") &&
    !parsed.username && !parsed.password && parsed.pathname === "/" &&
    !parsed.search && !parsed.hash && !/[\s?#]/u.test(raw) && !raw.endsWith("/");
}

export function playerFrameSource(template: string | undefined) {
  if (!template || template.split("{launchId}").length !== 2) {return null;}
  const raw = template.replace("{launchId}", exampleLaunchId);
  try {
    const parsed = new URL(raw);
    if (!isOriginOnly(parsed, raw)) {return null;}
    const labels = parsed.hostname.split(".");
    if (labels.length < 2 || labels[0] !== exampleLaunchId ||
      labels.slice(1).some((label) => !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/u.test(label))) {
      return null;
    }
    const port = parsed.port ? `:${parsed.port}` : "";
    return `'self' ${parsed.protocol}//*.${labels.slice(1).join(".")}${port}`;
  } catch {
    return null;
  }
}
