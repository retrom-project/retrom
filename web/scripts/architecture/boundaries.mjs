import path from "node:path";
import { inspectRuntimeBoundaries } from "./runtime-boundaries.mjs";
import { dependencyPaths, violation } from "./boundary-paths.mjs";
import { ownershipIndex } from "./ownership.mjs";

export function featureOf(name) {
  return name?.match(/^web\/features\/([^/]+)\//)?.[1] ?? null;
}

function layerOf(name) {
  return name?.match(/^web\/(app|features|components|lib)\//)?.[1] ?? null;
}

export function inspectWebBoundaries(files, registry) {
  if (!Array.isArray(files) || files.length === 0 || new Set(files.map((file) => file.file)).size !== files.length) {
    throw new Error("architecture: empty or duplicated web graph");
  }
  const owners = ownershipIndex(registry);
  const known = new Map(files.map((file) => [file.file, file]));
  const violations = [];
  const features = new Set();
  for (const source of files) {
    if (!owners.has(source.file)) {
      violations.push(violation("GOV01", source.file, 1, "compiled or discovered source lacks an explicit owner", [source.file]));
    }
    if (featureOf(source.file) && owners.get(source.file)?.kind === "production") {
      features.add(featureOf(source.file));
    }
    if (owners.get(source.file)?.kind !== "test") {
      violations.push(...inspectEdges(source, owners));
    }
    if (layerOf(source.file) && owners.get(source.file)?.kind !== "test") {
      violations.push(...inspectTransitiveLayers(source, known, owners));
    }
  }
  violations.push(...inspectPublicEntries(features, known));
  violations.push(...inspectFeatureCycles(files.filter((file) => owners.get(file.file)?.kind !== "test")),
    ...inspectRuntimeBoundaries(files, owners));
  return violations.sort((left, right) => left.file.localeCompare(right.file, "en") ||
    left.line - right.line || left.rule.localeCompare(right.rule, "en") || left.message.localeCompare(right.message, "en") ||
    left.dependencyChain.join("\0").localeCompare(right.dependencyChain.join("\0"), "en"));
}

function inspectPublicEntries(features, known) {
  const findings = [];
  for (const feature of [...features].sort()) {
    for (const entry of ["api.ts", "contracts.ts"]) {
      const filename = "web/features/" + feature + "/" + entry;
      if (!known.has(filename)) {
        findings.push(violation("WEB01", filename, 1, "Feature public entry is absent", [filename]));
      }
    }
    const contract = known.get("web/features/" + feature + "/contracts.ts");
    if (contract && (!contract.typed || contract.exports.some((item) => item.runtimeValue))) {
      findings.push(violation("WEB01", contract.file, 1, "Feature contracts must export only compiler-resolved types", [contract.file]));
    }
  }
  return findings;
}

function publicEntry(name) {
  const feature = featureOf(name);
  return feature && ["api.ts", "contracts.ts"].some((entry) => name === "web/features/" + feature + "/" + entry);
}

function privateFeatureEdge(source, edge) {
  const target = featureOf(edge.resolved);
  if (!target || target === featureOf(source.file) || publicEntry(edge.resolved)) {
    return null;
  }
  return ["api.ts", "contracts.ts"].includes(path.posix.basename(edge.resolved)) ?
    "nested api/contracts filename is not a Feature public entry" :
    "outside consumer imports a private Feature implementation";
}

function inspectEdges(source, owners) {
  const findings = [];
  for (const edge of source.imports) {
    const chain = [source.file, edge.resolved ?? edge.specifier];
    const add = (rule, message) => findings.push(violation(rule, source.file, edge.line, message, chain));
    if (edge.resolved === null && layerOf(source.file)) {
      add("WEB01", "variable module loading requires a verified dispatcher contract");
    }
    const privateImport = privateFeatureEdge(source, edge);
    if (privateImport) {
      add("WEB01", privateImport);
    }
    if (publicEntry(source.file) && edge.exportAll) {
      add("WEB01", "Feature public entry must explicitly select its exports");
    }
    if (owners.get(source.file)?.kind === "production" && owners.get(edge.resolved)?.kind === "test") {
      add("GOV01", "production import reaches a test source");
    }
  }
  return findings;
}

function inspectTransitiveLayers(source, known, owners) {
  const result = [];
  const initialLayer = layerOf(source.file);
  for (const chain of dependencyPaths(source.file, known)) {
    const target = chain.at(-1);
    const targetLayer = layerOf(target);
    const forbidden = (["lib", "components"].includes(initialLayer) && ["features", "app"].includes(targetLayer)) ||
      (initialLayer === "features" && targetLayer === "app");
    if (forbidden) {
      result.push(violation("WEB01", source.file, 1, "transitive import violates the frontend layer direction", chain));
    }
    const targetKind = owners.get(target)?.kind;
    if (owners.get(source.file)?.kind === "production" && ["test", "tool"].includes(targetKind)) {
      result.push(violation("GOV01", source.file, 1, "production import reaches test/tool implementation", chain));
    }
  }
  return result;
}

function inspectFeatureCycles(files) {
  const edges = new Map();
  for (const file of files) {
    const owner = featureOf(file.file);
    if (!owner) {
      continue;
    }
    if (!edges.has(owner)) {
      edges.set(owner, new Set());
    }
    for (const dependency of file.imports) {
      const target = featureOf(dependency.resolved);
      if (target && target !== owner) {
        edges.get(owner).add(target);
      }
    }
  }
  return [...edges.keys()].sort().flatMap((feature) => {
    const cycle = featureCycle(feature, edges);
    return cycle ? [violation("WEB01", `web/features/${feature}`, 1, "Feature dependency cycle", cycle)] : [];
  });
}

function featureCycle(origin, edges) {
  const queue = [[origin]];
  const seen = new Set([origin]);
  for (let index = 0; index < queue.length; index += 1) {
    const chain = queue[index];
    for (const next of [...(edges.get(chain.at(-1)) ?? [])].sort()) {
      if (next === origin) {
        return [...chain, origin];
      }
      if (!seen.has(next)) {
        seen.add(next);
        queue.push([...chain, next]);
      }
    }
  }
  return null;
}
