import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ImmersiveAudioPreferences } from "./immersive-audio-preferences";
import { useImmersiveBackgroundAudio } from "./use-immersive-background-audio";

const preferences: ImmersiveAudioPreferences = {
  bgmVolume: 0.4,
  bgmMuted: false,
  gameVolume: 1,
  gameMuted: false,
};

function BackgroundAudioHarness({ value = preferences }: { value?: ImmersiveAudioPreferences }) {
  const { audioRef, retry, state } = useImmersiveBackgroundAudio(value);
  return <div>
    <audio ref={audioRef} src="/audio/immersive/insert-coin.ogg" loop aria-label="测试背景音乐" />
    <output>{state}</output>
    <button type="button" onClick={() => void retry()}>重试</button>
  </div>;
}

function setVisibility(value: DocumentVisibilityState) {
  Object.defineProperty(document, "visibilityState", { configurable: true, value });
  document.dispatchEvent(new Event("visibilitychange"));
}

beforeEach(() => {
  setVisibility("visible");
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  setVisibility("visible");
});

describe("immersive background audio", () => {
  it("loops at the saved volume and stops when the shell unmounts", async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    const pause = vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
    const view = render(<BackgroundAudioHarness />);
    const audio = screen.getByLabelText<HTMLAudioElement>("测试背景音乐");
    expect(audio.loop).toBe(true);
    await waitFor(() => expect(play).toHaveBeenCalledOnce());
    expect(audio.volume).toBe(0.4);
    expect(screen.getByText("playing")).toBeInTheDocument();
    view.unmount();
    expect(pause).toHaveBeenCalled();
  });

  it("exposes an autoplay refusal and lets an explicit action retry", async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockRejectedValueOnce(new DOMException("blocked"));
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
    render(<BackgroundAudioHarness />);
    await waitFor(() => expect(screen.getByText("blocked")).toBeInTheDocument());
    play.mockResolvedValueOnce();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(screen.getByText("playing")).toBeInTheDocument());
  });

  it("pauses while hidden and resumes after visibility returns", async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    const pause = vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
    render(<BackgroundAudioHarness />);
    await waitFor(() => expect(play).toHaveBeenCalledOnce());
    act(() => setVisibility("hidden"));
    expect(pause).toHaveBeenCalled();
    expect(screen.getByText("paused")).toBeInTheDocument();
    act(() => setVisibility("visible"));
    await waitFor(() => expect(play).toHaveBeenCalledTimes(2));
  });

  it("does not play while background music is muted", async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => undefined);
    render(<BackgroundAudioHarness value={{ ...preferences, bgmMuted: true }} />);
    await waitFor(() => expect(screen.getByText("paused")).toBeInTheDocument());
    expect(play).not.toHaveBeenCalled();
  });
});
