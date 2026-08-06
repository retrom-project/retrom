import createClient from "openapi-fetch";
import type { paths } from "./generated/schema";

export const api = createClient<paths>({ baseUrl: "", credentials: "same-origin" });

export function writeHeaders(extra: Record<string, string> = {}) {
  return extra;
}
