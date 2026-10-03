import { StrictMode, useState } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { configureAuthenticatedClient } from "@/lib/api/client";
import { ImportBatchDiscard } from "./import-batch-discard";
import type { ImportDiscardStatus } from "./import-workflow";

const importId = "01980000-0000-7000-8000-000000000101";
const status = (state: ImportDiscardStatus["state"]): ImportDiscardStatus => ({ kind: "IMPORT", importId, state, errorCode: null });

function ControlledDiscard({ initial }: { initial: ImportDiscardStatus }) {
  const [disposition, setDisposition] = useState(initial);
  return <ImportBatchDiscard disposition={disposition} onChange={setDisposition} />;
}

describe("ImportBatchDiscard", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    configureAuthenticatedClient({ csrfToken: null, onAuthenticationFailure: null });
  });

  it("uses supplied status without reads on mount, rerender or remount in Strict Mode", async () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const view = render(<StrictMode><ImportBatchDiscard disposition={status("AVAILABLE")} onChange={vi.fn()} /></StrictMode>);
    expect(screen.getByRole("button", { name: "丢弃" })).toBeEnabled();
    view.rerender(<StrictMode><ImportBatchDiscard disposition={status("UNAVAILABLE")} onChange={vi.fn()} /></StrictMode>);
    expect(screen.getByRole("button", { name: "丢弃" })).toBeDisabled();
    view.unmount();
    render(<StrictMode><ImportBatchDiscard disposition={status("COMPLETED")} onChange={vi.fn()} /></StrictMode>);
    expect(screen.getByRole("status")).toHaveTextContent("未发布内容已丢弃");
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(fetch).not.toHaveBeenCalled();
  });

  it("confirms the exact batch and submits once without polling from the button", async () => {
    const requests: Request[] = [];
    configureAuthenticatedClient({ csrfToken: "test-csrf", onAuthenticationFailure: null });
    vi.stubGlobal("fetch", vi.fn(async (request: Request) => {
      requests.push(request);
      return Response.json(status("REQUESTED"));
    }));
    const user = userEvent.setup();
    render(<ControlledDiscard initial={status("AVAILABLE")} />);
    await user.click(screen.getByRole("button", { name: "丢弃" }));
    expect(screen.getByRole("alertdialog")).toHaveTextContent("已发布游戏和服务器原始文件保留");
    expect(requests).toHaveLength(0);
    await user.click(screen.getByRole("button", { name: "确认丢弃" }));
    expect(await screen.findByRole("button", { name: "正在丢弃…" })).toBeDisabled();
    expect(requests).toHaveLength(1);
    expect(requests[0].method).toBe("POST");
    expect(new URL(requests[0].url).pathname).toBe(`/api/v1/admin/import-batches/IMPORT/${importId}/discard`);
    expect(requests[0].headers.get("X-Retrom-Csrf")).toBe("test-csrf");
    expect(requests[0].headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("shows the supplied cleanup error and allows retry of the same disposition", async () => {
    const fetch = vi.fn(async () => Response.json(status("REQUESTED")));
    vi.stubGlobal("fetch", fetch);
    const user = userEvent.setup();
    render(<ControlledDiscard initial={{ ...status("FAILED"), errorCode: "IMPORT_BATCH_DISCARD_RELEASE_FAILED" }} />);
    expect(screen.getByRole("alert")).toHaveTextContent("源文件清理任务失败");
    await user.click(screen.getByRole("button", { name: "重试丢弃" }));
    await user.click(screen.getByRole("button", { name: "确认丢弃" }));
    expect(await screen.findByRole("button", { name: "正在丢弃…" })).toBeDisabled();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("keeps the current state actionable when the command fails", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => Response.json({ error: { message: "处置失败" } }, { status: 500 })));
    const user = userEvent.setup();
    render(<ControlledDiscard initial={status("AVAILABLE")} />);
    await user.click(screen.getByRole("button", { name: "丢弃" }));
    await user.click(screen.getByRole("button", { name: "确认丢弃" }));
    expect(await screen.findByText("丢弃请求未成功，请重试")).toBeVisible();
    expect(screen.getByRole("button", { name: "确认丢弃" })).toBeEnabled();
  });
});
