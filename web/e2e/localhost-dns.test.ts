// @vitest-environment node
import {createServer, get} from "node:http";
import type {AddressInfo} from "node:net";
import {expect, test} from "vitest";
import {installLocalhostDNS} from "./localhost-dns";

test("Node E2E requests reach loopback while retaining the isolated origin host", async () => {
  const server = createServer((request, response) => response.end(request.headers.host));
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const restore = installLocalhostDNS();
  try {
    const host = `retrom-app.rpg.localhost:${(server.address() as AddressInfo).port}`;
    const body = await new Promise<string>((resolve, reject) => {
      get(`http://${host}/`, (response) => {
        let body = "";
        response.setEncoding("utf8");
        response.on("data", (chunk: string) => {body += chunk;});
        response.on("end", () => resolve(body));
      }).on("error", reject);
    });
    expect(body).toBe(host);
  } finally {
    restore();
    await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  }
});
