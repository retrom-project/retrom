import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ServiceHealth } from "./service-health";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it("opens the compact ready result from an empty success response and restores keyboard focus", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 200 })));
  render(<ServiceHealth compact />);
  const trigger = await screen.findByRole("button", { name: "服务器状态：服务正常" });
  trigger.focus();
  fireEvent.click(trigger);
  const dialog = screen.getByRole("dialog", { name: "服务状态" });
  expect(within(dialog).getByRole("status")).toHaveTextContent("服务已通过就绪检查。");
  expect(trigger).toHaveAttribute("aria-expanded", "true");
  fireEvent.keyDown(document.activeElement!, { key: "Escape" });
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(trigger).toHaveFocus();
});

it("shows the current flat API error in the desktop fault details", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(
    { code: "SERVICE_UNAVAILABLE", message: "Service is temporarily unavailable" }, { status: 503 },
  )));
  render(<ServiceHealth />);
  await screen.findByText("服务存在异常");
  expect(screen.getByRole("tooltip")).toHaveTextContent("服务暂时无法完成就绪检查，请稍后再试。（HTTP 503）");
});

it("keeps the HTTP failure visible when an intermediary returns a non-JSON response", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("Bad gateway", { status: 502 })));
  render(<ServiceHealth compact />);
  fireEvent.click(await screen.findByRole("button", { name: "服务器状态：服务存在异常" }));
  expect(screen.getByRole("status")).toHaveTextContent("请求失败，请重试。（HTTP 502）");
});

it("shows a connection failure without claiming backend readiness", async () => {
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));
  render(<ServiceHealth compact />);
  fireEvent.click(await screen.findByRole("button", { name: "服务器状态：服务存在异常" }));
  expect(screen.getByRole("status")).toHaveTextContent("服务连接失败，请检查网络后刷新页面。");
});
