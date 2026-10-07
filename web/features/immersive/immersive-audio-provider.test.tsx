import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ImmersiveAudioProvider, useImmersiveAudio } from "./immersive-audio-provider";

function RouteContent({ name }: { name: string }) {
  const { commitPreference, preferences } = useImmersiveAudio();
  return <main>
    <h1>{name}</h1>
    <output aria-label="背景音乐音量">{preferences.bgmVolume}</output>
    <button type="button" onClick={() => commitPreference("bgm-volume", "right")}>提高音量</button>
  </main>;
}

beforeEach(() => {
  localStorage.clear();
  Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
  vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("ImmersiveAudioProvider", () => {
  it("keeps one playing audio element and its position while child routes change", async () => {
    const view = render(<ImmersiveAudioProvider><RouteContent name="沉浸入口" /></ImmersiveAudioProvider>);
    const audio = view.container.querySelector<HTMLAudioElement>('[data-immersive-bgm="true"]');
    expect(audio).not.toBeNull();
    await waitFor(() => expect(HTMLMediaElement.prototype.play).toHaveBeenCalledOnce());
    audio!.currentTime = 1;
    fireEvent.click(screen.getByRole("button", { name: "提高音量" }));
    await waitFor(() => expect(screen.getByLabelText("背景音乐音量")).toHaveTextContent("0.5"));
    const playCalls = vi.mocked(HTMLMediaElement.prototype.play).mock.calls.length;

    view.rerender(<ImmersiveAudioProvider><RouteContent name="平台游戏列表" /></ImmersiveAudioProvider>);

    expect(screen.getByRole("heading", { name: "平台游戏列表" })).toBeInTheDocument();
    expect(view.container.querySelector('[data-immersive-bgm="true"]')).toBe(audio);
    expect(audio?.currentTime).toBe(1);
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalledTimes(playCalls);
    expect(HTMLMediaElement.prototype.pause).not.toHaveBeenCalled();
  });
});
