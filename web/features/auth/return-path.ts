const publicRoutes = ["/login", "/setup", "/register", "/reset-password"];

export function isAuthenticationRoute(pathname: string): boolean {
  return publicRoutes.includes(pathname.replace(/\/+$/u, ""));
}

export function authenticationReturnPath(search: string): string {
  const value = new URLSearchParams(search).get("returnTo");
  if (
    !value ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    /[\\\u0000-\u001f\u007f]/u.test(value)
  ) {
    return "/";
  }
  const destination = new URL(value, "https://retrom.invalid");
  return isAuthenticationRoute(destination.pathname) ? "/" : value;
}
