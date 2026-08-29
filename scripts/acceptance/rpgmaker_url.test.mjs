import assert from "node:assert/strict";
import test from "node:test";

import { normalizedBase } from "./rpgmaker_url.mjs";

test("acceptance base URLs allow HTTPS and loopback localhost names", () => {
  for (const value of [
    "https://dev.sendev.cc",
    "http://localhost:13004",
    "http://127.0.0.1:13004",
    "http://app.rpg.localhost:13004",
  ]) {
    assert.equal(normalizedBase(value), value);
  }
});

test("acceptance base URLs reject insecure non-local and malformed origins", () => {
  for (const value of [
    "http://example.com",
    "http://user:password@localhost:13004",
    "http://localhost:13004/path",
    "http://localhost:13004/?query=1",
  ]) {
    assert.throws(() => normalizedBase(value));
  }
});
