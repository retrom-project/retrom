import {readFile, writeFile} from "node:fs/promises";
import {parseArgs} from "node:util";
import {readPFBProvider} from "./content_io_pfb_provider.mjs";
import {proofDigest} from "./content_io_case_proof.mjs";

const {values} = parseArgs({options: {case: {type: "string", multiple: true}, chrome: {type: "string"}, output: {type: "string"}}});
const catalog = JSON.parse(await readFile("tests/fixtures/content-io/product-cases.json", "utf8"));
const snapshot = {schemaVersion: 1, browser: {sha256: proofDigest(await readFile(values.chrome))}, cases: {}};
for (const id of values.case ?? []) {
  const definition = catalog.cases.find(row => row.caseId === id);
  if (!definition) throw new Error("CONTENT_IO_PRODUCT_CASE_INVALID");
  const provider = await readPFBProvider(process.cwd(), definition.providerId, definition.targetId);
  snapshot.cases[id] = {providerId: definition.providerId, targetId: definition.targetId,
    identities: provider.identities, developmentSha256: provider.developmentSha256, nativeAssets: provider.nativeAssets};
}
await writeFile(values.output, JSON.stringify(snapshot, null, 2) + "\n", {flag: "wx"});
