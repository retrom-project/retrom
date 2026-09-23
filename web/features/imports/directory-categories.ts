export { categoryForPlatform, directoryCategories } from "@/features/platforms/platform-category";
export type { DirectoryCategory } from "@/features/platforms/platform-category";

export type DirectoryChoice = {
  id: string;
  name: string;
  platformId?: string;
  platformName: string;
  coreName: string;
};

export function matchesDirectory(directory: DirectoryChoice, query: string): boolean {
  const words = query.trim().toLocaleLowerCase("zh-CN").split(/\s+/).filter(Boolean);
  if (!words.length) {return true;}
  const fields = [directory.name, directory.platformName, directory.coreName, directory.platformId ?? ""]
    .join(" ").toLocaleLowerCase("zh-CN");
  return words.every((word) => fields.includes(word));
}
