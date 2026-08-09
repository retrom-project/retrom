import "server-only";
import { cookies } from "next/headers";

const backend = process.env.NEXT_BACKEND_ORIGIN ?? "http://127.0.0.1:8080";

export async function backendJSON<T>(path: string): Promise<T> {
  const cookieStore = await cookies();
  const sessionCookies = ["retrom_session", "__Host-retrom_session"]
    .map((name) => cookieStore.get(name))
    .filter((cookie): cookie is { name: string; value: string } => Boolean(cookie))
    .map((cookie) => `${cookie.name}=${cookie.value}`)
    .join("; ");
  const headers = new Headers({ Accept: "application/json" });
  if (sessionCookies) headers.set("Cookie", sessionCookies);
  const response = await fetch(`${backend}${path}`, { cache: "no-store", headers });
  if (!response.ok) throw new Error(`Retrom API ${path} returned ${response.status}`);
  return response.json() as Promise<T>;
}
