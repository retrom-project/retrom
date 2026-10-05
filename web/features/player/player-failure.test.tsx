import {cleanup, render, screen} from "@testing-library/react";
import {afterEach, describe, expect, it, vi} from "vitest";
import {PlayerLoading} from "./player-loading";
import {readRuntimeFailure} from "./runtime/runtime-failure";
import type {RuntimeFailureV1} from "./runtime/contract";

afterEach(cleanup);

const failure: RuntimeFailureV1 = {code: "OPENBOR_CORE_EXITED", category: "CONTENT", phase: "STARTUP", retryable: false,
  diagnostics: [{source: "CORE", message: "Unknown constant V_TRANSPLENT in data/scripts/game.c"}]};
const props = {onRetry: vi.fn(), state: "error" as const, message: failure.code, progress: null,
  returnIntent: {kind: "GAME" as const, href: "/games/example"}};
describe("persistent failure panel", () => {
  it("shows native diagnostics and content guidance without offering a pointless retry", () => {
    render(<PlayerLoading {...props} failure={failure} />);
    expect(screen.getByRole("alert")).toHaveTextContent("游戏启动失败");
    expect(screen.getByText(/V_TRANSPLENT/u)).toBeVisible();
    expect(screen.queryByRole("button", {name: "重试启动"})).not.toBeInTheDocument();
    expect(screen.getByRole("link", {name: "返回游戏详情"})).toHaveAttribute("href", "/games/example");
  });
  it("distinguishes a gameplay crash from initialization failure", () => {
    render(<PlayerLoading {...props} failure={{...failure, phase: "PLAYING", category: "CORE", code: "OPENBOR_CORE_ABORTED"}} />);
    expect(screen.getByRole("alert")).toHaveTextContent("游戏运行中断");
  });
  it("retries explicitly transient failures", () => {
    render(<PlayerLoading {...props} failure={{...failure, category: "NETWORK", retryable: true}} />);
    expect(screen.getByRole("button", {name: "重试启动"})).toBeVisible();
  });
  it("rejects oversized diagnostics and copies values before teardown", () => {
    const copy = readRuntimeFailure(failure);
    expect(copy).toEqual(failure); expect(copy).not.toBe(failure); expect(copy.diagnostics).not.toBe(failure.diagnostics);
    expect(readRuntimeFailure({...failure, diagnostics: [{source: "CORE", message: "x".repeat(501)}]}).code).toBe("PLAYER_RUNTIME_CONTRACT_INVALID");
  });
});
