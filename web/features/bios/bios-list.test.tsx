import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { Schema } from "@/lib/api/types";
import { BiosList } from "./bios-list";

afterEach(cleanup);
const item: Schema<"BiosRequirement"> = {
  key: "runtime/shared/firmware.bin", name: "firmware.bin", platformIds: ["platform"],
  coreIds: ["core-a", "core-b"], required: true, installed: true,
  filename: "installed.bin", sizeBytes: 4096, sha256: "c".repeat(64),
  requirements: [
    { coreId: "core-a", sizeBytes: 16384, sha256: "a".repeat(64), md5: null },
    { coreId: "core-b", sizeBytes: 32768, sha256: null, md5: "b".repeat(32) },
  ],
};
function show(value = item) {
  render(<BiosList items={[value]} catalog={null} onInstall={vi.fn()} onRemove={vi.fn()} />);
  fireEvent.click(screen.getByText("文件校验要求"));
}

it("keeps each core's declared requirements distinct from the installed file facts", () => {
  show();
  const installed = screen.getByText("已安装文件").parentElement!;
  expect(within(installed).getByText("4,096 字节")).toBeVisible();
  expect(within(installed).getByText(item.sha256)).toBeVisible();
  const first = screen.getByRole("region", { name: "核心 core-a 的文件校验要求" });
  expect(within(first).getByText("16,384 字节")).toBeVisible();
  expect(within(first).getByText("a".repeat(64))).toBeVisible();
  expect(within(first).queryByText("MD5")).not.toBeInTheDocument();
  expect(within(first).queryByText(item.sha256)).not.toBeInTheDocument();
  const second = screen.getByRole("region", { name: "核心 core-b 的文件校验要求" });
  expect(within(second).getByText("32,768 字节")).toBeVisible();
  expect(within(second).getByText("b".repeat(32))).toBeVisible();
  expect(within(second).queryByText("SHA-256")).not.toBeInTheDocument();
});

it("does not invent validation values or installed files when metadata is unknown", () => {
  show({ ...item, installed: false, filename: "", sizeBytes: 0, sha256: "",
    requirements: [{ coreId: "core-a", sizeBytes: null, sha256: null, md5: null }] });
  expect(screen.getByText("未声明固定大小或校验值。")).toBeVisible();
  expect(screen.queryByText("已安装文件")).not.toBeInTheDocument();
  expect(screen.queryByText(/字节|SHA-256|MD5/)).not.toBeInTheDocument();
});
