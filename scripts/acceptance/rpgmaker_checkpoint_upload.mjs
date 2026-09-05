const maximumRequestBytes = 270 * 1024 * 1024;

// Playwright may omit Blob-backed multipart postData from Request. Chrome's
// getRequestPostData supplies the original bytes, with base64Encoded for binary.
// Observe one real request; do not install page hooks or change its transport.
export async function observeCheckpointUpload(page, previewId) {
  const session = await page.context().newCDPSession(page);
  const expectedUrl = new URL("/runtime/launches/" + previewId + "/save-states", page.url()).href;
  let captured = null;
  let duplicate = false;
  session.on("Network.requestWillBeSent", ({request, requestId}) => {
    if (request.method !== "POST" || request.url !== expectedUrl) {return;}
    if (captured) {duplicate = true; return;}
    const headers = Object.fromEntries(Object.entries(request.headers).map(([name, value]) => [name.toLowerCase(), value]));
    captured = {url: request.url, method: request.method, key: headers["idempotency-key"],
      body: session.send("Network.getRequestPostData", {requestId}).then(
        value => ({value}), () => ({error: true}),
      )};
  });
  await session.send("Network.enable", {maxPostDataSize: maximumRequestBytes});
  return {
    async take(request) {
      const headers = await request.allHeaders();
      if (duplicate || !captured || captured.url !== request.url() || captured.method !== request.method() ||
          !captured.key || captured.key !== headers["idempotency-key"]) {
        throw new Error("RPG_PREVIEW_CHECKPOINT_OBSERVATION_MISMATCH");
      }
      const response = await captured.body;
      if (response.error) {throw new Error("RPG_PREVIEW_CHECKPOINT_BODY_UNAVAILABLE");}
      return decodeCheckpointRequestBody(response.value);
    },
    async close() {await session.detach();},
  };
}

export function decodeCheckpointRequestBody(response) {
  if (typeof response?.postData !== "string" || response.postData.length > Math.ceil(maximumRequestBytes / 3) * 4 ||
      (response.base64Encoded !== undefined && typeof response.base64Encoded !== "boolean")) {
    throw new Error("RPG_PREVIEW_CHECKPOINT_BODY_INVALID");
  }
  const bytes = Buffer.from(response.postData, response.base64Encoded ? "base64" : "utf8");
  if (!bytes.length || bytes.length > maximumRequestBytes ||
      (response.base64Encoded && bytes.toString("base64") !== response.postData)) {
    throw new Error("RPG_PREVIEW_CHECKPOINT_BODY_INVALID");
  }
  return bytes;
}

export async function readCheckpointMultipart(request, wireBody) {
  const headers = await request.allHeaders();
  const body = wireBody ?? request.postDataBuffer();
  const length = Number(headers["content-length"]);
  if (!Buffer.isBuffer(body) || !Number.isSafeInteger(length) || length !== body.length || length > maximumRequestBytes) {
    throw new Error("RPG_PREVIEW_CHECKPOINT_REQUEST_INVALID");
  }
  const form = await new Response(body, {headers: {"Content-Type": headers["content-type"]}}).formData();
  return {form, length};
}
