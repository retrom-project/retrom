import { createHash } from "node:crypto";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// A source revision also works in release images without a Git checkout.
export function pwaRevision(root: string): string {
  const hash = createHash("sha256");
  function visit(directory: string) {
    for (const entry of readdirSync(join(root, directory), { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) {
        visit(path);
      } else if (!/\.(test|spec)\./u.test(entry.name)) {
        hash.update(path).update(readFileSync(join(root, path)));
      }
    }
  }
  for (const directory of ["app", "components", "features", "lib", "styles", "public/pwa"]) {
    visit(directory);
  }
  for (const path of ["package-lock.json", "next.config.ts", "proxy.ts"]) {
    hash.update(readFileSync(join(root, path)));
  }
  return hash.digest("hex").slice(0, 20);
}
