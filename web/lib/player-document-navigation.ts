const playerPath = /^\/play\/([0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$/;

type NavigationEnvironment = Pick<Location, "origin">;

export function replaceWithPlayerDocument(
  playURL: string,
  replace: (href: string) => void,
  environment: NavigationEnvironment = { origin: window.location.origin },
) {
  const target = new URL(playURL, environment.origin);
  if (target.origin !== environment.origin || !playerPath.test(target.pathname)) {
    throw new Error("启动响应包含无效地址");
  }
  const route = `${target.pathname}${target.search}`;
  replace(route);
}
