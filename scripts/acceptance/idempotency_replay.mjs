import assert from "node:assert/strict";

export async function replayable(client, report, method, path, data, status, headers = {}) {
  const options = {headers: {...client.writeHeaders(), ...headers}, data};
  const first = await client.raw(method, path, options);
  assert.equal(first.status(), status, `IDEMPOTENCY_ORIGINAL_${method}_${status}`);
  const body = await first.text(), originalHeaders = first.headers();
  report.operations.push({method, route: path.replace(/[0-9a-f]{8}-[0-9a-f-]{27,}/gu, "{id}"), status});
  const replay = async () => {
    const response = await client.raw(method, path, options);
    assert.equal(response.status(), status, "IDEMPOTENCY_REPLAY_STATUS");
    assert.equal(await response.text(), body, "IDEMPOTENCY_REPLAY_BYTES");
    assert.equal(response.headers()["x-retrom-idempotent-replay"], "true", "IDEMPOTENCY_REPLAY_HEADER");
    for (const name of ["etag", "location", "content-type"]) {
      assert.equal(response.headers()[name], originalHeaders[name], `IDEMPOTENCY_REPLAY_${name}`);
    }
    return response;
  };
  await replay();
  return {value: body ? JSON.parse(body) : null, replay, options, original: first};
}

export async function finalizeUpload(client,report,id) {
 const session=await client.raw("GET",`/api/v1/admin/uploads/${id}`);
 assert.equal(session.status(),200,"IDEMPOTENCY_UPLOAD_STATE");
 return replayable(client,report,"POST",`/api/v1/admin/uploads/${id}/complete`,undefined,202,
  {"If-Match":session.headers().etag});
}
