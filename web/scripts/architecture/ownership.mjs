import path from "node:path";
import ts from "typescript";
import { checkedText } from "./source-inputs.mjs";

const baseline = "82834bade1648da3067ebda1b1fc89c18577fd6a";

function exactKeys(value, fields) {
  return value && !Array.isArray(value) && Object.keys(value).sort().join(",") === [...fields].sort().join(",");
}

function safePath(value) {
  return typeof value === "string" && value.length > 0 && !value.includes("\\") &&
    !path.posix.isAbsolute(value) && value.split("/").every((part) => part !== "." && part !== ".." && part !== "");
}

export function ownershipIndex(registry) {
  if (!exactKeys(registry, ["schemaVersion", "baseline", "packages"]) || registry.schemaVersion !== 1 ||
    registry.baseline !== baseline || !Array.isArray(registry.packages) || registry.packages.length === 0) {
    throw new Error("architecture: invalid ownership registry");
  }
  const owners = new Map();
  const packages = new Set();
  for (const entry of registry.packages) {
    validatePackage(entry, packages);
    packages.add(entry.path);
    for (const file of entry.files) {
      validateFile(file, entry, owners);
      owners.set(file.path, { ...file, layer: entry.layer, module: entry.module, owner: entry.owner });
    }
  }
  return owners;
}

function validatePackage(entry, packages) {
  if (!exactKeys(entry, ["path", "layer", "module", "owner", "files"]) ||
    (entry.path !== "." && !safePath(entry.path)) || packages.has(entry.path) ||
    typeof entry.layer !== "string" || typeof entry.module !== "string" || !/^RF(?:0[1-9]|1[0-9]|2[0-2])$/.test(entry.owner) ||
    !Array.isArray(entry.files) || entry.files.length === 0) {
    throw new Error("architecture: invalid or duplicated package ownership");
  }
}

function validateFile(file, entry, owners) {
  if (!exactKeys(file, ["path", "kind"]) || !safePath(file.path) || path.posix.dirname(file.path) !== entry.path ||
    owners.has(file.path) || !["production", "test", "tool", "generated"].includes(file.kind)) {
    throw new Error("architecture: invalid or duplicated file ownership");
  }
}

export function readOwnership(root) {
  const input = checkedText(root, "quality/architecture/package-ownership.json");
  const registry = JSON.parse(input);
  const syntax = ts.parseJsonText("ownership.json", input);
  const visit = (node) => {
    if (ts.isObjectLiteralExpression(node)) {
      const keys = node.properties.map((property) => property.name.text);
      if (new Set(keys).size !== keys.length) {
        throw new Error("architecture: duplicate JSON key in ownership registry");
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(syntax);
  ownershipIndex(registry);
  return registry;
}
