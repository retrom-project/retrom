import { ApiError } from "@/lib/api/client";
import type { Directory, Schema } from "@/lib/api/types";
import { recommendedDirectoryTemplates } from "./recommended-directory-templates";

type DirectoryInput = Schema<"DirectoryWriteRequest">;
export function recommendedDirectories(catalog: Schema<"RuntimeCatalog">): DirectoryInput[] {
  return recommendedDirectoryTemplates.flatMap(([platformId, defaultCoreId, name, description = ""]) => {
    const available = catalog.platforms.some((platform) => platform.id === platformId)
      && catalog.cores.some((core) => core.id === defaultCoreId && core.fingerprint !== "" && core.platformIds.includes(platformId));
    if (!available) { return []; }
    return [{ platformId, defaultCoreId, name, description, coreIds: [defaultCoreId], enabled: true,
      slug: `${platformId}-${defaultCoreId}`.replaceAll("_", "-") }];
  });
}

function covered(template: DirectoryInput, directories: Directory[]) {
  return directories.some((directory) => directory.slug === template.slug || directory.platformId === template.platformId
    && (directory.defaultCoreId === template.defaultCoreId || directory.name.trim() === template.name));
}

export async function createRecommendedDirectories(templates: DirectoryInput[], actions: {
  list: () => Promise<Directory[]>;
  create: (input: DirectoryInput) => Promise<Directory>;
}, signal: AbortSignal) {
  let existing = await actions.list();
  const summary = { created: 0, existing: 0, failures: [] as Array<{ name: string; message: string }> };
  for (const template of templates) {
    if (signal.aborted) { break; }
    if (covered(template, existing)) { summary.existing++; continue; }
    try {
      existing.push(await actions.create(template));
      summary.created++;
    } catch (failure) {
      // Directory conflicts have no dedicated name code. Verify the current row before calling it a skip.
      if (failure instanceof ApiError && failure.status === 409) {
        try { existing = await actions.list(); } catch { /* Keep the original failure for retry. */ }
        if (covered(template, existing)) { summary.existing++; continue; }
      }
      summary.failures.push({ name: template.name, message: failure instanceof Error ? failure.message : "创建失败" });
      if (failure instanceof ApiError && [401, 403].includes(failure.status)) { break; }
    }
  }
  return summary;
}
