const prefix = "retrom:v2:user:";

export function userStoragePrefix(userId: string) {
  return `${prefix}${userId}:`;
}

export function userStorageKey(userId: string | null | undefined, feature: string, key: string) {
  if (!userId) return null;
  return `${userStoragePrefix(userId)}${feature}:${key}`;
}

export function clearUserStorage(userId: string | null | undefined) {
  if (!userId || typeof window === "undefined") return;
  const expected = userStoragePrefix(userId);
  for (const storage of [window.localStorage, window.sessionStorage]) {
    const keys = Array.from({ length: storage.length }, (_, index) => storage.key(index))
      .filter((key): key is string => Boolean(key?.startsWith(expected)));
    for (const key of keys) storage.removeItem(key);
  }
}
