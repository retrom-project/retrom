import {afterEach, expect, it, vi} from "vitest";
import {requestPlayerFullscreen} from "./player-fullscreen";

afterEach(() => vi.restoreAllMocks());

function phone(active = false) {
  vi.spyOn(navigator, "userAgent", "get").mockReturnValue("Android Mobile");
  const lock = vi.fn().mockResolvedValue(undefined);
  vi.spyOn(window, "screen", "get").mockReturnValue({orientation: {
    type: "portrait-primary", lock,
  }} as unknown as Screen);
  let fullscreen: Element | null = active ? document.documentElement : null;
  const request = vi.fn(async () => {fullscreen = document.documentElement;});
  const properties: Array<[object, string, PropertyDescriptor | undefined]> = [];
  function property(target: object, key: string, descriptor: PropertyDescriptor) {
    properties.push([target, key, Object.getOwnPropertyDescriptor(target, key)]);
    Object.defineProperty(target, key, {configurable: true, ...descriptor});
  }
  property(document, "fullscreenEnabled", {value: true});
  property(document, "fullscreenElement", {get: () => fullscreen});
  property(document.documentElement, "requestFullscreen", {value: request});
  return {lock, request, restore: () => {
    for (const [target, key, descriptor] of properties) {
      if (descriptor) {Object.defineProperty(target, key, descriptor);}
      else {Reflect.deleteProperty(target, key);}
    }
  }};
}

it("requests phone landscape after entering fullscreen, before presenting a manual rotation fallback", async () => {
  const fixture = phone();
  try {
    await expect(requestPlayerFullscreen()).resolves.toBe(document.documentElement);
    expect(fixture.lock).toHaveBeenCalledExactlyOnceWith("landscape");
    expect(fixture.request.mock.invocationCallOrder[0]).toBeLessThan(fixture.lock.mock.invocationCallOrder[0]);
  } finally {fixture.restore();}
});

it("attempts phone rotation in existing fullscreen while preserving its ownership", async () => {
  const fixture = phone(true);
  try {
    await expect(requestPlayerFullscreen()).resolves.toBeNull();
    expect(fixture.request).not.toHaveBeenCalled();
    expect(fixture.lock).toHaveBeenCalledExactlyOnceWith("landscape");
  } finally {fixture.restore();}
});

it("keeps successful fullscreen when orientation locking is unsupported", async () => {
  const fixture = phone();
  fixture.lock.mockRejectedValue(new DOMException("Unsupported", "NotSupportedError"));
  try {
    await expect(requestPlayerFullscreen()).resolves.toBe(document.documentElement);
    expect(fixture.lock).toHaveBeenCalledOnce();
  } finally {fixture.restore();}
});
