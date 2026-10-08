import type { components } from "./generated/schema";
export type Schema<Name extends keyof components["schemas"]> =
  components["schemas"][Name];
export type Game = Schema<"Game">;
export type Save = Schema<"Save">;
export type Directory = Schema<"Directory">;
export type Tag = Schema<"Tag">;
