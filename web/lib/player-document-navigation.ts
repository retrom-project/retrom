const playerPath = /^\/play\/([0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/;

type DocumentLocation = Pick<Location, "origin" | "replace">;

export function replaceWithPlayerDocument(playURL: string, location: DocumentLocation = window.location) {
  const target = new URL(playURL, location.origin);
  if (target.origin !== location.origin || !playerPath.test(target.pathname)) {
    throw new Error("启动响应包含无效地址");
  }
  location.replace(target.href);
}
