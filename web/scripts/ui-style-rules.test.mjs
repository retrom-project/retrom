import assert from "node:assert/strict";
import test from "node:test";
import { checkComponent, checkGlobalImports, checkStyles } from "./ui-style-rules.mjs";

const tokens = new Set(["--text-body", "--weight-regular", "--font-sans"]);
test("rejects the audited small bold controls, including responsive overrides", () => {
  const css = "@media(max-width:900px){.editor select{font-size:10px;font-weight:760;height:35px;background:red}}";
  assert.ok(checkStyles(css, "features/editor.css", tokens).length >= 6);
});
test("rejects token shadowing, important, shorthand and undeclared tokens", () => {
  for (const css of [".page{--text-body:10px}", ".page{--radius-cover:16px}", ".page{font-size:var(--text-body)!important}", ".page{font:800 10px serif}", ".page{font-size:var(--invented)}"]) {
    assert.ok(checkStyles(css, "features/page.css", tokens).length > 0);
  }
});
test("control tokens cannot be used to camouflage per-page control overrides", () => {
  assert.ok(checkStyles(".page select{font-size:var(--text-body)}", "features/page.css", tokens).length > 0);
  assert.equal(checkStyles("select{font-size:var(--text-body)}", "styles/controls.css", tokens).length, 0);
});
test("allows layout, semantic typography and shared control definitions", () => {
  assert.deepEqual(checkStyles(".page{display:grid;gap:24px}.page h2{font-size:var(--text-body)}.page .button{width:100%}", "features/page.css", tokens), []);
});
test("rejects global styles escaping the control layer", () => {
  assert.ok(checkGlobalImports('@import "../features/new.css";').length > 0);
  assert.deepEqual(checkGlobalImports('@layer app, controls; @import "../styles/tokens.css"; @import "../features/new.css" layer(app); @import "../styles/controls.css" layer(controls);'), []);
});

test("distinguishes native selects from selection-button class names", () => {
  assert.deepEqual(checkStyles(".favorite-select { height: 38px; border: 1px solid white; }", "features/favorites/favorites.css", new Set()), []);
  assert.ok(checkStyles(".toolbar > select { height: 30px; }", "features/library/library.css", new Set()).length > 0);
});

test("rejects JSX typography in literal, expression, template and inline styles", () => {
  for (const source of ['<p className="text-xs" />', '<p className={"font-black"} />', '<p className={`panel ${active ? "text-sm" : "text-lg"}`} />', '<p style={{ "fontSize": 10 }} />']) {
    assert.ok(checkComponent(source, "page.tsx").length > 0, source);
  }
  assert.deepEqual(checkComponent('<p className={active ? "button" : "button secondary"} />', "page.tsx"), []);
});
