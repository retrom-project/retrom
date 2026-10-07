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
  return value;
}
