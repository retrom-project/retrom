import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { configureAuthenticatedClient } from "@/lib/api/client";
import { ImportBatchDiscard } from "./import-batch-discard";

const importId = "01980000-0000-7000-8000-000000000101";

describe("ImportBatchDiscard", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    configureAuthenticatedClient({ csrfToken: null, onAuthenticationFailure: null });
  });

  it("confirms the exact batch, submits once and refreshes when background disposition completes", async () => {
    const requests: Request[] = [];
    let state = "AVAILABLE";
    configureAuthenticatedClient({ csrfToken: "test-csrf", onAuthenticationFailure: null });
    vi.stubGlobal("fetch", vi.fn(async (request: Request) => {
      requests.push(request);
      if (request.method === "POST") { state = "REQUESTED"; }
      return Response.json({ kind: "PEGASUS", importId, state, errorCode: null });
    }));
    const completed = vi.fn();
    const user = userEvent.setup();
    render(<ImportBatchDiscard kind="PEGASUS" importId={importId} onCompleted={completed} />);
    await waitFor(() => expect(screen.getByRole("button", { name: "丢弃" })).toBeEnabled());
    await user.click(screen.getByRole("button", { name: "丢弃" }));
    expect(screen.getByRole("alertdialog")).toHaveTextContent("已发布游戏和服务器原始文件保留");
    expect(requests.filter((request) => request.method === "POST")).toHaveLength(0);
    await user.click(screen.getByRole("button", { name: "确认丢弃" }));
    expect(await screen.findByRole("button", { name: "正在丢弃…" })).toBeDisabled();
    const writes = requests.filter((request) => request.method === "POST");
    expect(writes).toHaveLength(1);
    expect(new URL(writes[0].url).pathname).toBe(`/api/v1/admin/import-batches/PEGASUS/${importId}/discard`);
    expect(writes[0].headers.get("X-Retrom-Csrf")).toBe("test-csrf");
    expect(writes[0].headers.get("Idempotency-Key")).toBeTruthy();
    state = "COMPLETED";
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("未发布内容已丢弃"), { timeout: 2_000 });
    expect(completed).toHaveBeenCalledTimes(1);
  });

  it("restores completed progress after reload without repeating the action", async () => {
    const fetch = vi.fn(async () => Response.json({ kind: "IMPORT", importId, state: "COMPLETED", errorCode: null }));
    vi.stubGlobal("fetch", fetch);
    render(<ImportBatchDiscard kind="IMPORT" importId={importId} />);
    expect(await screen.findByRole("status")).toHaveTextContent("未发布内容已丢弃");
    expect(screen.getByRole("button", { name: "丢弃" })).toBeDisabled();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("keeps a failed batch actionable and allows retry of the same disposition", async () => {
    vi.stubGlobal("fetch", vi.fn(async (request: Request) => Response.json({
      kind: "EMULATIONSTATION", importId, state: request.method === "POST" ? "REQUESTED" : "FAILED",
      errorCode: request.method === "POST" ? null : "IMPORT_BATCH_DISCARD_FAILED",
    })));
    const user = userEvent.setup();
    render(<ImportBatchDiscard kind="EMULATIONSTATION" importId={importId} />);
    await user.click(await screen.findByRole("button", { name: "重试丢弃" }));
    await user.click(screen.getByRole("button", { name: "确认丢弃" }));
    expect(await screen.findByRole("button", { name: "正在丢弃…" })).toBeDisabled();
  });
  it("disables discard when every item already has a final decision", async () => {
    const fetch = vi.fn(async () => Response.json({ kind: "IMPORT", importId, state: "UNAVAILABLE", errorCode: null }));
    vi.stubGlobal("fetch", fetch);
    const user = userEvent.setup();
    render(<ImportBatchDiscard kind="IMPORT" importId={importId} />);
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    const button = screen.getByRole("button", { name: "丢弃" });
    expect(button).toBeDisabled();
    await user.click(button);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

});
