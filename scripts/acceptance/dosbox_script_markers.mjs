import assert from "node:assert/strict";

function locate(source, expression, code) {
  const matches = [...source.matchAll(expression)]; assert.equal(matches.length, 1, code);
  const match = matches[0], prefix = source.slice(0, match.index), lines = prefix.split("\n");
  return {match, lineNumber: lines.length - 1, columnNumber: lines.at(-1).length};
}

export function dosOwnerMarker(source) {
  const {match, lineNumber, columnNumber} = locate(source, /this\.cleanupFlycast\s*=\s*([\w$]+)\.cleanup\s*[;,]/gu,
    "DOS_CONTENT_OWNER_BREAKPOINT_AMBIGUOUS");
  return {lineNumber, columnNumber, variable: match[1]};
}

export function dosFailureMarker(source) {
  const {match} = locate(source,
    /fail\s*\(\s*([\w$]+)\s*,\s*[\w$]+\s*\)\s*\{\s*(?:if\s*\(\s*)?this\.state\s*===\s*"FAILED"/gu,
    "DOS_FAILURE_BREAKPOINT_AMBIGUOUS");
  let offset = match.index + match[0].indexOf("{") + 1;
  offset += /^\s*/u.exec(source.slice(offset))[0].length;
  const lines = source.slice(0, offset).split("\n");
  return {lineNumber: lines.length - 1, columnNumber: lines.at(-1).length, variable: match[1]};
}
