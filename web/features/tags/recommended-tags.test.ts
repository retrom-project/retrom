import { expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/client";
import { createRecommendedTags, recommendedTags } from "./recommended-tags";

it("creates missing recommendations across active-tag pages, then skips every existing tag", async () => {
  const active = Array.from({ length: 100 }, (_, index) => ({ name: `自定义 ${index}` }));
  active.push({ name: "　动作冒险\n" });
  const original = structuredClone(active);
  const actions = {
    list: vi.fn(async (offset: number) => ({ items: active.slice(offset, offset + 100), total: active.length })),
    create: vi.fn(async (name: string) => { active.push({ name }); }),
  };
  // A previously deleted recommendation is absent from the active list and gets a new ordinary POST.
  expect(await createRecommendedTags(actions, new AbortController().signal)).toMatchObject({ created: 9, existing: 1, failures: [] });
  expect(actions.list.mock.calls.map(([offset]) => offset)).toEqual([0, 100]);
  expect(active.slice(0, original.length)).toEqual(original);
  actions.create.mockClear();
  expect(await createRecommendedTags(actions, new AbortController().signal)).toMatchObject({ created: 0, existing: 10, failures: [] });
  expect(actions.create).not.toHaveBeenCalled();
});

it("counts only the exact concurrent name conflict as existing and retries failed names", async () => {
  const active: Array<{ name: string }> = [];
  let first = true;
  const actions = {
    list: async () => ({ items: [...active], total: active.length }),
    create: vi.fn(async (name: string) => {
      if (first && name === recommendedTags[0]) {
        active.push({ name });
        throw new ApiError("TAG_NAME_CONFLICT", "name exists", 409);
      }
      if (first && name === recommendedTags[1]) { throw new ApiError("VERSION_CONFLICT", "conflict", 409); }
      active.push({ name });
    }),
  };
  const firstSummary = await createRecommendedTags(actions, new AbortController().signal);
  expect(firstSummary).toMatchObject({ created: 8, existing: 1 });
  expect(firstSummary.failures).toHaveLength(1);
  first = false;
  actions.create.mockClear();
  expect(await createRecommendedTags(actions, new AbortController().signal)).toMatchObject({ created: 1, existing: 9, failures: [] });
  expect(actions.create).toHaveBeenCalledExactlyOnceWith(recommendedTags[1]);
});
