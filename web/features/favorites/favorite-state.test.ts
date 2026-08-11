import { describe, expect, it } from "vitest";
import { favoriteQuery, favoriteQueryString, selectFavoriteScope, toggleGameSelection } from "./favorite-state";

describe("favorite state", () => {
  it("preserves invalid folder scopes for a recoverable server error and normalizes irrelevant folders", () => {
    const invalidFolder = favoriteQuery({ scope: "FOLDER", folderId: "bad" });
    expect(invalidFolder).toMatchObject({ scope: "FOLDER", folderId: "bad" });
    expect(favoriteQueryString(invalidFolder)).toBe("scope=FOLDER&limit=50");
    expect(favoriteQuery({ scope: "UNCATEGORIZED", folderId: "01980000-0000-7000-8000-000000000001" }))
      .toMatchObject({ scope: "UNCATEGORIZED", folderId: "" });
  });

  it("serializes only the stable non-default URL state", () => {
    const query = favoriteQuery({
      scope: "FOLDER", folderId: "01980000-0000-7000-8000-000000000001",
      q: "  arcade ", platformId: "arcade", sort: "TITLE_ASC",
    });
    expect(favoriteQueryString(query)).toBe("scope=FOLDER&folderId=01980000-0000-7000-8000-000000000001&q=arcade&platformId=arcade&sort=TITLE_ASC&limit=50");
    expect(selectFavoriteScope(query, "ALL").folderId).toBe("");
    expect(Array.from(favoriteQuery({ q: "😀".repeat(201) }).q)).toHaveLength(200);
  });

  it("toggles batch selection without mutating the previous set", () => {
    const previous = new Set(["a"]);
    const next = toggleGameSelection(previous, "b");
    expect([...previous]).toEqual(["a"]);
    expect([...next]).toEqual(["a", "b"]);
    expect([...toggleGameSelection(next, "a")]).toEqual(["b"]);
  });
});
