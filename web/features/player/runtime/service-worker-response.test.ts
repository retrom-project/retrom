// @vitest-environment node
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { createContext, runInContext } from "node:vm";
import { expect, it } from "vitest";

type Message = {
  id: number;
  type: string;
  path: string;
  offset?: number;
  length?: number;
};
type Port = {
  onmessage: ((event: { data: unknown }) => void) | null;
  postMessage: (message: Message) => void;
  start: () => void;
  close: () => void;
};
function workerResponse() {
  const bytes = new TextEncoder().encode("postMessage('ready');");
  const port: Port = {
    onmessage: null,
    postMessage(message) {
      const data =
        message.type === "STAT"
          ? {
              id: message.id,
              status: 200,
              path: message.path,
              sizeBytes: bytes.length,
              mediaType: "text/javascript",
            }
          : {
              id: message.id,
              status: 200,
              bytes: bytes.slice(
                message.offset ?? 0,
                (message.offset ?? 0) + (message.length ?? bytes.length),
              ),
            };
      port.onmessage?.({ data });
    },
    start() {},
    close() {},
  };
  const context = createContext({
    self: {
      location: {
        href: "http://isolated.test/__retrom/runtime-isolation/run/service-worker.js",
      },
      addEventListener() {},
    },
    URL,
    Headers,
    Response,
    ReadableStream,
    TextEncoder,
    TextDecoder,
    Uint8Array,
    setTimeout,
    clearTimeout,
  });
  runInContext(
    readFileSync(
      fileURLToPath(
        new URL(
          "../../../public/runtime-isolation/service-worker.js",
          import.meta.url,
        ),
      ),
      "utf8",
    ),
    context,
  );
  const bind = context.bind as (
    port: Port,
    config: { entryFile: string; parentOrigin: string },
  ) => void;
  bind(port, { entryFile: "index.html", parentOrigin: "http://host.test" });
  return context.serve as (request: Request, path: string) => Promise<Response>;
}
it("allows a project Worker response to inherit the document's strict embedder policy", async () => {
  const serve = workerResponse();
  const request = new Request("http://isolated.test/run/run/js/decoder.js");
  Object.defineProperty(request, "destination", { value: "worker" });
  const response = await serve(request, "js/decoder.js");
  expect(response.status).toBe(200);
  expect(await response.text()).toBe("postMessage('ready');");
  expect(response.headers.get("Cross-Origin-Resource-Policy")).toBe(
    "same-origin",
  );
  expect(response.headers.get("Cross-Origin-Embedder-Policy")).toBe(
    "require-corp",
  );
  expect(response.headers.get("Content-Security-Policy")).toContain(
    "connect-src 'self' blob:",
  );
  expect(response.headers.get("Content-Security-Policy")).toContain(
    "worker-src 'self' blob:",
  );
});
