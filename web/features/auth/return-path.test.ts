import { expect, it } from "vitest";
import { authenticationReturnPath } from "./return-path";

it("returns to the requested protected page after login", () => {
  expect(
    authenticationReturnPath(
      "?returnTo=%2Flibrary%2Fgames%2Fgame-id%3Ffrom%3Dfavorites",
    ),
  ).toBe("/library/games/game-id?from=favorites");
});

it.each(["https://example.com/", "//example.com/", "/\\example.com/", "/\n"])(
  "keeps unsafe login destinations on the home page: %s",
  (path) => {
    expect(authenticationReturnPath(`?returnTo=${encodeURIComponent(path)}`)).toBe(
      "/",
    );
  },
);

it.each(["/login", "/setup?returnTo=%2Flibrary", "/register#token=unused", "/reset-password", "/games/../login/"])(
  "does not return an authenticated user to another authentication page: %s", (path) => {
    expect(authenticationReturnPath(`?returnTo=${encodeURIComponent(path)}`)).toBe("/");
  },
);
