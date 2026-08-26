import { describe, expect, it, vi } from "vitest";
import { replaceWithPlayerDocument } from "./player-document-navigation";

const launchId = "0198abcd-1234-7123-8abc-1234567890ab";

describe("replaceWithPlayerDocument", () => {
  it("performs a full same-origin document navigation to the player", () => {
    const replace = vi.fn();
    replaceWithPlayerDocument(`/play/${launchId}?experience=immersive`, {
      origin: "https://retrom.example",
      replace,
    });

    expect(replace).toHaveBeenCalledWith(
      `https://retrom.example/play/${launchId}?experience=immersive`,
    );
  });

  it.each([
    "https://runtime.invalid/play/0198abcd-1234-7123-8abc-1234567890ab",
    "/games/0198abcd-1234-7123-8abc-1234567890ab",
    "/play/not-a-launch",
  ])("rejects an invalid player destination: %s", (playURL) => {
    expect(() => replaceWithPlayerDocument(playURL, {
      origin: "https://retrom.example",
      replace: vi.fn(),
    })).toThrow("启动响应包含无效地址");
  });
});
