import { expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/client";
import type { Directory, Schema } from "@/lib/api/types";
import { createRecommendedDirectories, recommendedDirectories } from "./recommended-directories";

const catalog: Schema<"RuntimeCatalog"> = {
  platforms: ["nes", "snes", "j2me", "rpgmaker", "gba"].map((id) => ({ id, name: id })),
  cores: [
    { id: "fceumm", name: "NES", fingerprint: "installed", platformIds: ["nes"] },
    { id: "snes9x", name: "SNES", fingerprint: "installed", platformIds: ["snes"] },
    { id: "j2me", name: "Java", fingerprint: "", platformIds: ["j2me"] },
    { id: "rpgmaker", name: "RPG Maker", fingerprint: null, platformIds: ["rpgmaker"] },
  ], providers: [], bindings: [],
};
function directory(input: Schema<"DirectoryWriteRequest">, id = input.slug): Directory {
  return { ...input, id, version: 1, gameCount: 0 };
}

it("recommends only installed platform/core pairs while retaining valid multi-target cores", () => {
  expect(recommendedDirectories(catalog).map((item) => item.platformId)).toEqual(["nes", "snes", "rpgmaker"]);
  expect(recommendedDirectories({ ...catalog, cores: [] })).toEqual([]);
  expect(recommendedDirectories({ ...catalog, platforms: [] })).toEqual([]);
});

it("preserves customized and disabled recommendations and makes repeat clicks idempotent", async () => {
  const templates = recommendedDirectories(catalog);
  const customized = { ...directory(templates[0]), name: "我的精选", defaultCoreId: "other-nes", coreIds: ["other-nes"], enabled: false };
  const equivalent = { ...directory(templates[1]), name: "收藏目录", slug: "my-custom-snes" };
  const active = [customized, equivalent];
  const preserved = structuredClone(active);
  const actions = {
    list: async () => [...active],
    create: vi.fn(async (input: Schema<"DirectoryWriteRequest">) => { const item = directory(input); active.push(item); return item; }),
  };
  expect(await createRecommendedDirectories(templates, actions, new AbortController().signal)).toMatchObject({ created: 1, existing: 2, failures: [] });
  expect(active.slice(0, 2)).toEqual(preserved);
  actions.create.mockClear();
  expect(await createRecommendedDirectories(templates, actions, new AbortController().signal)).toMatchObject({ created: 0, existing: 3, failures: [] });
  expect(actions.create).not.toHaveBeenCalled();
});

it("confirms concurrent creations by rereading instead of treating every 409 as success", async () => {
  const templates = recommendedDirectories(catalog).slice(0, 2);
  const active: Directory[] = [];
  const summary = await createRecommendedDirectories(templates, {
    list: async () => [...active],
    create: async (input) => {
      if (input.platformId === "nes") { active.push(directory(input)); }
      throw new ApiError("VERSION_CONFLICT", "conflict", 409);
    },
  }, new AbortController().signal);
  expect(summary).toMatchObject({ created: 0, existing: 1 });
  expect(summary.failures.map((item) => item.name)).toEqual([templates[1].name]);
});

it("does not create remaining directories after navigation aborts or permission is lost", async () => {
  const controller = new AbortController();
  const templates = recommendedDirectories(catalog);
  const create = vi.fn(async (input: Schema<"DirectoryWriteRequest">) => { controller.abort(); return directory(input); });
  expect(await createRecommendedDirectories(templates, { list: async () => [], create }, controller.signal)).toMatchObject({ created: 1 });
  expect(create).toHaveBeenCalledTimes(1);
  const denied = vi.fn(async () => { throw new ApiError("FORBIDDEN", "forbidden", 403); });
  expect((await createRecommendedDirectories(templates, { list: async () => [], create: denied }, new AbortController().signal)).failures).toHaveLength(1);
  expect(denied).toHaveBeenCalledTimes(1);
});
