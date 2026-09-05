import {createRef} from "react";
import {render} from "@testing-library/react";
import {describe, expect, it} from "vitest";
import {PlayerStage} from "./player-shell";

describe("PlayerStage", () => {
  it("keeps the provider mount outside React-owned loading content", () => {
    const runtimeTarget = createRef<HTMLDivElement>();
    const view = render(<PlayerStage blocked={false} stage={runtimeTarget} state="loading"
      message="loading" loadProgress={null} returnTo="/library" immersive={false}
      onSurface={() => undefined} />);

    expect(runtimeTarget.current).toHaveClass("player-runtime-mount");
    const canvas = document.createElement("canvas");
    runtimeTarget.current?.replaceChildren(canvas);

    expect(() => view.rerender(<PlayerStage blocked={false} stage={runtimeTarget} state="running"
      message="running" loadProgress={null} returnTo="/library" immersive={false}
      onSurface={() => undefined} />)).not.toThrow();
    expect(runtimeTarget.current?.firstElementChild).toBe(canvas);
  });
});
