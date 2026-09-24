import postcss from "postcss";
import ts from "typescript";

export const controlAppearance = new Set([
  "font", "font-size", "font-weight", "font-family", "line-height", "height", "min-height",
  "padding", "padding-inline", "padding-block", "border", "border-color", "border-radius",
  "background", "background-color", "background-image", "color", "box-shadow", "appearance",
]);

export function isSharedControl(selector) {
  return /(?:\.button|\.import-directory-trigger|(?:^|[\s>+~])select)(?:\.[\w-]+|:(?:hover|focus|focus-visible|disabled|active))*$/.test(selector.trim());
}

export function checkStyles(css, file, tokens) {
  const issues = [];
  const tree = postcss.parse(css, { from: file });
  const central = file === "styles/controls.css";
  const tokenSource = file === "styles/tokens.css";
  function report(node, message) { issues.push(`${file}:${node.source?.start?.line ?? 1}: ${message}`); }
  tree.walkAtRules("layer", (node) => {
    if (file !== "app/globals.css") { report(node, "Only globals.css may order application layers"); }
  });
  tree.walkDecls((node) => {
    if (tokenSource) { return; }
    if (/^--(?:text-|weight-|control-|font-|radius-)/.test(node.prop)) {
      report(node, "Typography, radius and control tokens must be defined in styles/tokens.css");
    }
    if (["font-size", "font-weight", "font-family"].includes(node.prop)) {
      const match = /^var\((--[\w-]+)\)$/.exec(node.value);
      if (!match || !tokens.has(match[1])) { report(node, `${node.prop} must reference a shared token`); }
      if (node.important) { report(node, "Typography cannot use !important"); }
    }
    if (node.prop === "font" && node.value !== "inherit") { report(node, "Use typography tokens instead of font shorthand"); }
    if (node.prop === "all") { report(node, "Reset individual layout properties instead of resetting typography and controls"); }
    if (!central && node.parent.type === "rule" && controlAppearance.has(node.prop)) {
      const selectors = postcss.list.comma(node.parent.selector);
      if (selectors.some(isSharedControl)) { report(node, "Shared control appearance belongs in styles/controls.css; feature CSS owns layout only"); }
    }
  });
  return issues;
}

export function checkGlobalImports(css) {
  const tree = postcss.parse(css);
  const imports = [];
  tree.walkAtRules("import", (node) => imports.push(node.params));
  const issues = [];
  for (const entry of imports.filter((value) => value.startsWith('"..'))) {
    if (entry.includes("tokens.css")) { continue; }
    const expected = entry.includes("controls.css") ? "layer(controls)" : "layer(app)";
    if (!entry.endsWith(expected)) { issues.push(`Global import must use ${expected}: ${entry}`); }
  }
  if (!css.includes("@layer app, controls;") || !imports.some((entry) => entry.includes("controls.css"))) {
    issues.push("Global stylesheet must declare app, controls layers and import shared controls");
  }
  return issues;
}

export function checkComponent(source, filename) {
  const tree = ts.createSourceFile(filename, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const issues = [];
  const utilities = /(?:^|\s)(?:[\w-]+:)*(?:text-(?:xs|sm|base|lg|xl|\d+xl|\[)|font-(?:thin|light|normal|medium|semibold|bold|black|\[))/;
  function checkClassName(node) {
    if ((ts.isStringLiteralLike(node) || ts.isTemplateHead(node) || ts.isTemplateMiddle(node) || ts.isTemplateTail(node)) && utilities.test(node.text)) {
      issues.push(`${filename}: Use shared typography instead of independent utility sizes/weights`);
    }
    ts.forEachChild(node, checkClassName);
  }
  function visit(node) {
    if (ts.isPropertyAssignment(node) && /^(fontSize|fontWeight|fontFamily)$/.test(ts.isStringLiteral(node.name) ? node.name.text : node.name.getText(tree))) {
      issues.push(`${filename}: Move inline typography to shared CSS tokens`);
    }
    if (ts.isJsxAttribute(node) && node.name.getText(tree) === "className" && node.initializer) { checkClassName(node.initializer); }
    ts.forEachChild(node, visit);
  }
  visit(tree);
  return issues;
}
