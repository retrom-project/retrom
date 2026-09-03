import type { components } from "@/lib/api/generated/schema";

export type ValidationCheckpointReceipt = Pick<
  components["schemas"]["ValidationCheckpointCreated"],
  "checkpointFormat" | "sizeBytes" | "sha256"
>;

export function parseValidationCheckpointReceipt(contents: string): ValidationCheckpointReceipt {
  let value: unknown;
  try {value = JSON.parse(contents);}
  catch {throw new Error("RPG_CHECKPOINT_RESPONSE_INVALID");}
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("RPG_CHECKPOINT_RESPONSE_INVALID");
  }
  const response = value as Partial<components["schemas"]["ValidationCheckpointCreated"]>;
  const keys = Object.keys(value).sort().join(",");
  const sizeBytes = Number(response.sizeBytes);
  if (!validResponseIdentity(keys, response) || !validResponseValues(response, sizeBytes)) {
    throw new Error("RPG_CHECKPOINT_RESPONSE_INVALID");
  }
  return { checkpointFormat: response.checkpointFormat, sizeBytes, sha256: response.sha256 };
}

function validResponseIdentity(
  keys: string,
  response: Partial<components["schemas"]["ValidationCheckpointCreated"]>,
) {
  return keys === "checkpointFormat,createdAtMs,resourceKind,sha256,sizeBytes,validationId" &&
    response.resourceKind === "RPG_RUNTIME_VALIDATION_CHECKPOINT" && canonicalUuid(response.validationId);
}

function validResponseValues(
  response: Partial<components["schemas"]["ValidationCheckpointCreated"]>,
  sizeBytes: number,
): response is components["schemas"]["ValidationCheckpointCreated"] {
  return typeof response.checkpointFormat === "string" && response.checkpointFormat.length >= 1 &&
    Number.isSafeInteger(sizeBytes) && sizeBytes >= 1 && sizeBytes <= 268_435_456 &&
    typeof response.sha256 === "string" && /^[0-9a-f]{64}$/.test(response.sha256) &&
    Number.isSafeInteger(response.createdAtMs) && Number(response.createdAtMs) >= 0;
}

function canonicalUuid(value: unknown) {
  return typeof value === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value);
}
