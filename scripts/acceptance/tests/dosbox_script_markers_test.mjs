import assert from "node:assert/strict";
import {test} from "node:test";
import {dosOwnerMarker, dosFailureMarker} from "../dosbox_script_markers.mjs";

test("DOS owner observation locates renamed minified locals at their exact column", () => {
  const source = 'const x=1;this.cleanupFlycast=a.cleanup,this.checkMountActive();';
  assert.deepEqual(dosOwnerMarker(source), {lineNumber: 0, columnNumber: 10, variable: "a"});
});
test("DOS owner observation supports multiline verified module bytes", () => {
  assert.deepEqual(dosOwnerMarker('class P {\n  this.cleanupFlycast = disc.cleanup;\n}'), {lineNumber: 1, columnNumber: 2, variable: "disc"});
});
test("DOS fatal observation exposes the actual minified code parameter", () => {
  const source = 'x(){}fail(e,r){this.state==="FAILED"||this.state==="EXITED"||this.close(e)}';
  assert.deepEqual(dosFailureMarker(source), {lineNumber: 0, columnNumber: 15, variable: "e"});
});
test("DOS fatal observation also handles a formatted method", () => {
  const source = '  fail(code, error) {\n    if (this.state === "FAILED" || this.state === "EXITED") {return;}\n}';
  assert.deepEqual(dosFailureMarker(source), {lineNumber: 1, columnNumber: 4, variable: "code"});
});
test("ambiguous or missing markers cannot silently observe another method", () => {
  for (const marker of [dosOwnerMarker, dosFailureMarker]) assert.throws(() => marker(''), /BREAKPOINT_AMBIGUOUS/);
  assert.throws(() => dosOwnerMarker('this.cleanupFlycast=a.cleanup;this.cleanupFlycast=b.cleanup;'), /BREAKPOINT_AMBIGUOUS/);
});
