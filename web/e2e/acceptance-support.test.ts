import { describe, expect, it, vi } from "vitest";
import { retryOnceOnConnectionReset } from "./acceptance-support";

describe("retryOnceOnConnectionReset", () => {
  it("retries one transient connection reset", async () => {
    const operation = vi.fn()
      .mockRejectedValueOnce(Object.assign(new Error("read ECONNRESET"), { code: "ECONNRESET" }))
      .mockResolvedValue("ready");

    await expect(retryOnceOnConnectionReset(operation, async () => undefined)).resolves.toBe("ready");
    expect(operation).toHaveBeenCalledTimes(2);
  });

  it("does not retry non-network failures", async () => {
    const failure = new Error("HTTP 401");
    const operation = vi.fn().mockRejectedValue(failure);

    await expect(retryOnceOnConnectionReset(operation, async () => undefined)).rejects.toBe(failure);
    expect(operation).toHaveBeenCalledTimes(1);
  });

  it("does not hide a second connection reset", async () => {
    const failure = Object.assign(new Error("read ECONNRESET"), { code: "ECONNRESET" });
    const operation = vi.fn().mockRejectedValue(failure);

    await expect(retryOnceOnConnectionReset(operation, async () => undefined)).rejects.toBe(failure);
    expect(operation).toHaveBeenCalledTimes(2);
  });
});
