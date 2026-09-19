import { execFileSync } from "node:child_process";
import { builtinModules } from "node:module";
import path from "node:path";
import ts from "typescript";

const sourcePattern = /\.(?:ts|tsx|js|jsx|mjs|cjs)$/;
const builtins = new Set(builtinModules.flatMap((name) => [name, `node:${name}`]));

export function sourcePaths(root) {
  const output = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard", "-z"], {
    cwd: root,
    encoding: "utf8",
  });
  return [...new Set(output.split("\0").filter((name) => name.startsWith("web/") && sourcePattern.test(name)))].sort();
}

export function readProject(root) {
  const configPath = path.join(root, "web", "tsconfig.json");
  const input = ts.readConfigFile(configPath, ts.sys.readFile);
  if (input.error) {
    throw new Error(ts.flattenDiagnosticMessageText(input.error.messageText, "\n"));
  }
  const parsed = ts.parseJsonConfigFileContent(input.config, ts.sys, path.dirname(configPath), {}, configPath);
  assertDiagnostics(parsed.errors);
  if (parsed.fileNames.length === 0) {
    throw new Error("architecture: empty TypeScript project");
  }
  return parsed;
}

export function inspectWeb(root) {
  const project = readProject(root);
  const program = ts.createProgram(project.fileNames, project.options);
  assertDiagnostics(ts.getPreEmitDiagnostics(program));
  const sources = sourcePaths(root);
  if (sources.length === 0) {
    throw new Error("architecture: empty Web source set");
  }
  const checker = program.getTypeChecker();
  return sources.map((name) => {
    const absolute = path.join(root, name);
    const text = ts.sys.readFile(absolute);
    if (text === undefined) {
      throw new Error(`architecture: missing source ${name}`);
    }
    const source = program.getSourceFile(absolute) ?? ts.createSourceFile(absolute, text, ts.ScriptTarget.Latest, true);
    assertDiagnostics(source.parseDiagnostics);
    return {
      file: name,
      package: path.posix.dirname(name),
      typed: program.getSourceFile(absolute) !== undefined,
      directives: directives(source),
      imports: inspectImports(root, source, project.options),
      exports: exportedSymbols(source, checker, program),
    };
  });
}

export function inspectImports(root, source, options) {
  const imports = [];
  const visit = (node) => {
    const dependency = importSpecifier(node);
    if (dependency) {
      const position = source.getLineAndCharacterOfPosition(node.getStart(source));
      imports.push({
        specifier: dependency.text,
        kind: dependency.kind,
        line: position.line + 1,
        resolved: resolveImport(root, source.fileName, dependency.text, options),
      });
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return imports.sort((left, right) => left.line - right.line || left.specifier.localeCompare(right.specifier, "en"));
}

function importSpecifier(node) {
  if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) {
    if (node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
      return { text: node.moduleSpecifier.text, kind: ts.isExportDeclaration(node) ? "export" : "import" };
    }
  }
  if (ts.isImportTypeNode(node) && ts.isLiteralTypeNode(node.argument) && ts.isStringLiteral(node.argument.literal)) {
    return { text: node.argument.literal.text, kind: "type" };
  }
  return callSpecifier(node);
}

function callSpecifier(node) {
  if (ts.isCallExpression(node) && node.arguments.length === 1 && ts.isStringLiteral(node.arguments[0])) {
    const dynamicImport = node.expression.kind === ts.SyntaxKind.ImportKeyword;
    const requireCall = ts.isIdentifier(node.expression) && node.expression.text === "require";
    if (dynamicImport || requireCall) {
      return { text: node.arguments[0].text, kind: dynamicImport ? "dynamic" : "require" };
    }
  }
  return null;
}

function resolveImport(root, containingFile, specifier, options) {
  if (builtins.has(specifier)) {
    return specifier;
  }
  // Next's createServerOnlyClientOnlyAliases supplies these exact boundary markers.
  if (specifier === "server-only" || specifier === "client-only") {
    return `framework:${specifier}`;
  }
  const result = ts.resolveModuleName(specifier, containingFile, options, ts.sys).resolvedModule;
  if (result) {
    return repositoryPath(root, result.resolvedFileName);
  }
  // CSS and image imports are resources covered by Next's ambient declarations.
  if (/\.(?:css|png|jpg|jpeg|svg|webp|woff2?|ogg|mp3)$/.test(specifier)) {
    return `asset:${specifier}`;
  }
  throw new Error(`architecture: unresolved import ${specifier} in ${repositoryPath(root, containingFile)}`);
}

function repositoryPath(root, filename) {
  const relative = path.relative(root, filename).split(path.sep).join("/");
  if (relative === ".." || relative.startsWith("../") || path.isAbsolute(relative)) {
    throw new Error("architecture: dependency outside repository");
  }
  return relative;
}

function exportedSymbols(source, checker, program) {
  if (program.getSourceFile(source.fileName) !== source) {
    return [];
  }
  const symbol = checker.getSymbolAtLocation(source);
  if (!symbol) {
    return [];
  }
  return checker.getExportsOfModule(symbol).map((item) => ({
    name: item.name,
    alias: (item.flags & ts.SymbolFlags.Alias) !== 0,
  })).sort((left, right) => left.name.localeCompare(right.name, "en"));
}

function directives(source) {
  return source.statements.filter((statement) => ts.isExpressionStatement(statement) &&
    ts.isStringLiteral(statement.expression)).map((statement) => statement.expression.text);
}

function assertDiagnostics(diagnostics) {
  const errors = diagnostics.filter((diagnostic) => diagnostic.category === ts.DiagnosticCategory.Error);
  if (errors.length > 0) {
    throw new Error(errors.map((item) => `TS${item.code}: ${ts.flattenDiagnosticMessageText(item.messageText, "\n")}`).join("\n"));
  }
}
