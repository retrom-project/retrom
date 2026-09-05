// Executed only by the acceptance browser, never shipped to the application.
// Read the original File/Blob objects at send time; do not replace, serialize,
// await, or otherwise change the FormData supplied to the native XHR.
export function installCheckpointUploadObservation(previewId) {
  const key = "__retromCheckpointUploadObservation";
  if (globalThis[key]) {throw new Error("RPG_PREVIEW_CHECKPOINT_OBSERVER_ALREADY_INSTALLED");}
  const expectedUrl = new URL("/runtime/launches/" + previewId + "/save-states", location.href).href;
  const prototype = XMLHttpRequest.prototype;
  const original = {open: prototype.open, send: prototype.send, setRequestHeader: prototype.setRequestHeader};
  const requests = new WeakMap();
  let record = null;
  let failure = null;
  prototype.open = function (...args) {
    requests.set(this, {method: String(args[0]).toUpperCase(), url: new URL(String(args[1]), location.href).href});
    return Reflect.apply(original.open, this, args);
  };
  prototype.setRequestHeader = function (...args) {
    const request = requests.get(this);
    if (request && String(args[0]).toLowerCase() === "idempotency-key") {request.idempotencyKey = String(args[1]);}
    return Reflect.apply(original.setRequestHeader, this, args);
  };
  prototype.send = function (...args) {
    const request = requests.get(this);
    if (request?.method === "POST" && request.url === expectedUrl) {
      try {
        if (record || !(args[0] instanceof FormData)) {throw new Error("unexpected upload");}
        const parts = Array.from(args[0].entries());
        const bytes = parts.reduce((total, [, value]) => total + (value instanceof Blob ? value.size : value.length * 4), 0);
        if (parts.length !== 3 || bytes > 270 * 1024 * 1024) {throw new Error("upload bound");}
        record = {...request, parts};
      } catch {failure = "RPG_PREVIEW_CHECKPOINT_OBSERVATION_INVALID";}
    }
    return Reflect.apply(original.send, this, args);
  };
  globalThis[key] = {
    async take() {
      if (failure || !record) {throw new Error(failure ?? "RPG_PREVIEW_CHECKPOINT_OBSERVATION_MISSING");}
      const captured = record;
      record = null;
      const parts = await Promise.all(captured.parts.map(async ([name, value]) => {
        if (!(value instanceof Blob)) {return {name, text: value};}
        const bytes = new Uint8Array(await value.arrayBuffer());
        let encoded = "";
        for (let offset = 0; offset < bytes.length; offset += 32768) {
          encoded += String.fromCharCode(...bytes.subarray(offset, offset + 32768));
        }
        return {name, fileName: value.name, type: value.type || "application/octet-stream",
          sizeBytes: bytes.length, base64: btoa(encoded)};
      }));
      return {...captured, parts};
    },
    dispose() {
      prototype.open = original.open;
      prototype.send = original.send;
      prototype.setRequestHeader = original.setRequestHeader;
      record = null;
      delete globalThis[key];
    },
  };
}

export async function readCheckpointMultipart(request, observed) {
  const headers = await request.allHeaders();
  const body = request.postDataBuffer();
  const length = Number(headers["content-length"]);
  if (!body || !Number.isSafeInteger(length) || length < body.length || length > 270 * 1024 * 1024) {
    throw new Error("RPG_PREVIEW_CHECKPOINT_REQUEST_INVALID");
  }
  const form = await new Response(body, {headers: {"Content-Type": headers["content-type"]}}).formData();
  if (!observed) {
    if (length !== body.length) {throw new Error("RPG_PREVIEW_CHECKPOINT_OBSERVATION_MISSING");}
    return {form, length};
  }
  if (observed.url !== request.url() || observed.method !== request.method() ||
      !observed.idempotencyKey || observed.idempotencyKey !== headers["idempotency-key"] ||
      !Array.isArray(observed.parts) || observed.parts.length !== 3) {
    throw new Error("RPG_PREVIEW_CHECKPOINT_OBSERVATION_MISMATCH");
  }
  const entries = Array.from(form.entries());
  const names = new Set(entries.map(([name]) => name));
  if (entries.length !== 3 || names.size !== 3 || !["metadata", "payload", "screenshot"].every(name => names.has(name))) {
    throw new Error("RPG_PREVIEW_CHECKPOINT_PARTS_INVALID");
  }
  let missingBytes = 0;
  const complete = new FormData();
  for (let index = 0; index < entries.length; index += 1) {
    const [name, value] = entries[index];
    const part = observed.parts[index];
    if (!(value instanceof Blob) || part.name !== name || part.fileName !== value.name || part.type !== value.type ||
        !Number.isSafeInteger(part.sizeBytes) || part.sizeBytes <= 0 || part.sizeBytes > length ||
        typeof part.base64 !== "string" || part.base64.length > Math.ceil(length / 3) * 4) {
      throw new Error("RPG_PREVIEW_CHECKPOINT_PART_MISMATCH");
    }
    const bytes = Buffer.from(part.base64, "base64");
    if (bytes.length !== part.sizeBytes || bytes.toString("base64") !== part.base64) {
      throw new Error("RPG_PREVIEW_CHECKPOINT_PART_BYTES_INVALID");
    }
    if (value.size === 0) {missingBytes += bytes.length;}
    else if (!Buffer.from(await value.arrayBuffer()).equals(bytes)) {
      throw new Error("RPG_PREVIEW_CHECKPOINT_PART_BYTES_MISMATCH");
    }
    complete.append(name, new Blob([bytes], {type: part.type}), part.fileName);
  }
  if (body.length + missingBytes !== length) {throw new Error("RPG_PREVIEW_CHECKPOINT_LENGTH_MISMATCH");}
  return {form: complete, length};
}
