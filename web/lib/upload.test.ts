import { afterEach, describe, expect, it, vi } from "vitest";

import { waitForJob } from "./upload";

describe("waitForJob", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("polls running jobs at one-second intervals", async () => {
    vi.useFakeTimers();
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ state: "RUNNING", phase: "SCRAPING" }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ state: "SUCCEEDED" }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const resultPromise = waitForJob("job-1");
    await vi.advanceTimersByTimeAsync(999);
    expect(fetchMock).toHaveBeenCalledOnce();
    await vi.advanceTimersByTimeAsync(1);
    await expect(resultPromise).resolves.toMatchObject({ state: "SUCCEEDED" });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
