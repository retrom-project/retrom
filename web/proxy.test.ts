// @vitest-environment node
import { unstable_doesMiddlewareMatch } from "next/experimental/testing/server";
import { describe, expect, it } from "vitest";
import { config } from "./proxy";

const matchesProxy = (url: string) => unstable_doesMiddlewareMatch({ config, nextConfig: {}, url });

describe("authentication proxy matcher", () => {
  it.each(["/icon.svg", "/icon.svg?revision=abc"])("serves the favicon without authentication: %s", url => {
    expect(matchesProxy(url)).toBe(false);
  });

  it.each(["/", "/library", "/admin/users", "/icon.svg/private", "/iconXsvg", "/icon.svg-extra"])("keeps page and lookalike paths behind authentication: %s", url => {
    expect(matchesProxy(url)).toBe(true);
  });
});
