import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ReviewValidationGuidance, reviewCompatibilityLabel } from "./review-validation-guidance";

describe("review validation guidance", () => {
  afterEach(cleanup);

  it("turns a missing BIOS blocker into a named action", () => {
    render(<ReviewValidationGuidance status="BLOCKED" compatibilityCode="LAUNCH_BIOS_MISSING" snapshot={{ bios: [{ logicalName: "panafz10.bin", requirementMode: "REQUIRED", blobId: null }] }} />);

    expect(screen.getByText("缺少必需 BIOS 文件")).toBeVisible();
    expect(screen.getByText("panafz10.bin")).toBeVisible();
    expect(screen.getByRole("link", { name: "安装所需 BIOS 文件" })).toHaveAttribute("href", "/admin/bios?scope=FULL_CATALOG&status=MISSING&q=panafz10.bin");
    expect(screen.getByText(/无需重新导入游戏/)).toBeVisible();
  });

  it("links an arcade BIOS blocker to the exact dependency archive", () => {
    const { container } = render(<ReviewValidationGuidance status="BLOCKED" compatibilityCode="LAUNCH_BIOS_MISSING" snapshot={{ dependencies: [{ kind: "BIOS_OR_BASE", machine: "stvbios", state: "MISSING", requiredEntries: ["epr19730.ic8"] }], missingEntries: ["stvbios.zip"] }} />);

    expect(screen.getByText("stvbios.zip")).toBeVisible();
    expect(screen.getByRole("link", { name: "安装所需 BIOS 文件" })).toHaveAttribute("href", "/admin/bios?scope=FULL_CATALOG&status=MISSING&q=stvbios.zip");
    expect(container.querySelector(".feedback-banner > i")).toBeNull();
  });

  it("keeps unknown blockers explicit instead of saying only needs inspection", () => {
    render(<ReviewValidationGuidance status="BLOCKED" compatibilityCode="CORE_CONTENT_FORMAT_UNSUPPORTED" />);
    expect(screen.getByText("运行检查未通过")).toBeVisible();
    expect(screen.getByText("CORE_CONTENT_FORMAT_UNSUPPORTED")).toBeVisible();
    expect(reviewCompatibilityLabel("LAUNCH_BIOS_MISSING", "BLOCKED")).toBe("缺少必需 BIOS 文件");
  });

  it("makes long missing-entry details keyboard scrollable", () => {
    const missingEntries = Array.from({ length: 13 }, (_, index) => `archive-${index + 1}.zip`);
    render(<ReviewValidationGuidance status="BLOCKED" compatibilityCode="ARCADE_CONTENT_MISSING_ENTRY" snapshot={{ missingEntries }} />);

    const region = screen.getByRole("region", { name: "运行检查错误详情，可滚动查看" });
    expect(region).toHaveAttribute("tabindex", "0");
    expect(region.querySelectorAll("li")).toHaveLength(13);
  });

  it("treats an unavailable Arcade DAT as a release dependency failure", () => {
    render(<ReviewValidationGuidance status="BLOCKED" compatibilityCode="ARCADE_DAT_UNAVAILABLE" />);

    expect(screen.getByText(/内置 Arcade DAT 尚未准备完成/)).toBeVisible();
    expect(screen.getByText("make prepare-deps")).toBeVisible();
    expect(screen.queryByRole("link", { name: /街机数据目录/ })).not.toBeInTheDocument();
  });
});
