import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import NetplayError from "@/app/netplay/error";
import NetplayLoading from "@/app/netplay/loading";
import NetplayRoomError from "@/app/netplay/rooms/[roomId]/error";
import NetplayRoomLoading from "@/app/netplay/rooms/[roomId]/loading";

describe("netplay route states", () => {
  afterEach(cleanup);

  it("announces both loading shells without exposing skeletons", () => {
    const first = render(<NetplayLoading />);
    expect(screen.getByRole("status")).toHaveTextContent("正在加载联机房间");
    first.unmount();
    render(<NetplayRoomLoading />);
    expect(screen.getByRole("status")).toHaveTextContent("正在同步房间状态");
  });

  it("offers retry actions for list and room failures", () => {
    const reset = vi.fn();
    const first = render(<NetplayError error={new Error("offline")} reset={reset} />);
    fireEvent.click(screen.getByRole("button", { name: "重新加载" }));
    expect(reset).toHaveBeenCalledOnce();
    first.unmount();
    render(<NetplayRoomError error={new Error("offline")} reset={reset} />);
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(reset).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("link", { name: "返回联机首页" })).toHaveAttribute("href", "/netplay");
  });
});
