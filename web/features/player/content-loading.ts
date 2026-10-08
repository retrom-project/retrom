import type { Schema } from "@/lib/api/types";
export type ContentLoading = "ON_DEMAND" | "PRELOAD";
export function targetContentLoading(
  catalog: Schema<"RuntimeCatalog"> | null,
  coreId: string,
  config: Schema<"RuntimeConfig">,
) {
  const binding = catalog?.bindings.find(
    (item) =>
      item.coreId === coreId &&
      item.contentKinds.includes(config.content.kind) &&
      (!config.content.engine || item.engine === config.content.engine),
  );
  const target = catalog?.providers
    .find((item) => item.providerId === binding?.providerId)
    ?.targets.find((item) => item.id === binding?.targetId);
  const capabilities = target?.capabilities;
  if (
    capabilities &&
    typeof capabilities === "object" &&
    "contentLoading" in capabilities
  ) {
    const value: unknown = capabilities.contentLoading;
    if (value === "ON_DEMAND_AND_PRELOAD" || value === "PRELOAD_ONLY") {
      return value;
    }
  }
  return null;
}
