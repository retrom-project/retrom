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
});
