import { dependencyPaths, violation } from "./boundary-paths.mjs";

function importsMarker(file, specifier) {
  return file.imports.some((edge) => !edge.typeOnly && edge.specifier === specifier);
}

function browserOnly(file) {
  return file.file === "web/lib/api/browser-client.ts" || importsMarker(file, "client-only");
}

function serverOnly(file) {
  return file.file === "web/lib/api/server-client.ts" || importsMarker(file, "server-only") ||
    importsMarker(file, "next/headers") || file.directives.includes("use server");
}

function serverEntry(file) {
  return !file.directives.includes("use client") && (serverOnly(file) ||
    /^web\/app\/.*(?:page|layout|route|template|default|loading|not-found)\.[jt]sx?$/.test(file.file));
}

export function inspectRuntimeBoundaries(files, owners) {
  const known = new Map(files.map((file) => [file.file, file]));
  const results = [];
  for (const source of files) {
    if (owners.get(source.file)?.kind === "test") {
      continue;
    }
    if (source.directives.includes("use client") || browserOnly(source)) {
      for (const chain of [[source.file], ...dependencyPaths(source.file, known, { runtime: true })]) {
        if (serverOnly(known.get(chain.at(-1)))) {
          results.push(violation("WEB02", source.file, 1, "client execution reaches server-only code", chain));
        }
      }
    }
    if (serverEntry(source)) {
      const paths = dependencyPaths(source.file, known, {
        runtime: true,
        stop: (file) => file.directives.includes("use client") && !browserOnly(file),
      });
      for (const chain of [[source.file], ...paths]) {
        if (browserOnly(known.get(chain.at(-1)))) {
          results.push(violation("WEB02", source.file, 1, "server execution reaches browser-only state", chain));
        }
      }
    }
  }
  return results;
}
