import { describe, expect, it, vi } from "vitest";
import { replaceWithPlayerDocument } from "./player-document-navigation";

const launchId = "0198abcd-1234-7123-8abc-1234567890ab";

describe("replaceWithPlayerDocument", () => {
  it("soft-navigates in the trusted-click fullscreen document", () => {
    const replace = vi.fn();
    replaceWithPlayerDocument(
      `/play/${launchId}?experience=immersive`,
      replace,
      { origin: "https://retrom.example" },
    );

    expect(replace).toHaveBeenCalledWith(`/play/${launchId}?experience=immersive`);
  });

  it.each([
    "https://runtime.invalid/play/0198abcd-1234-7123-8abc-1234567890ab",
    "/games/0198abcd-1234-7123-8abc-1234567890ab",
    "/play/not-a-launch",
  ])("rejects an invalid player destination: %s", (playURL) => {
    expect(() => replaceWithPlayerDocument(
      playURL,
      vi.fn(),
      { origin: "https://retrom.example" },
    )).toThrow("启动响应包含无效地址");
  });
});
