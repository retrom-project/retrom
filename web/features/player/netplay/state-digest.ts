import {sha256} from "@/lib/crypto";

export async function digestNetplayState(value: Uint8Array) {
  const digest = await sha256(value);
  return [...digest].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}

export function equalNetplayState(left: Uint8Array, right: Uint8Array) {
  return left.byteLength === right.byteLength && left.every((value, index) => value === right[index]);
}
