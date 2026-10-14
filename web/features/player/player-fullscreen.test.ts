import {afterEach, expect, it, vi} from "vitest";
import {beginPlayerPresentation, requestPlayerFullscreen} from "./player-fullscreen";

afterEach(() => vi.restoreAllMocks());

function phone(active = false) {
  vi.spyOn(navigator, "userAgent", "get").mockReturnValue("Android Mobile");
  const lock = vi.fn().mockResolvedValue(undefined);
  const unlock = vi.fn();
  vi.spyOn(window, "screen", "get").mockReturnValue({orientation: {
    type: "portrait-primary", lock, unlock,
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
  const exit = vi.fn(async () => {fullscreen = null;});
  property(document, "exitFullscreen", {value: exit});
  const session = beginPlayerPresentation();
  return {lock, unlock, request, exit, session, fullscreen: () => fullscreen, restore: () => {
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


it.each([false, true])("restores fullscreen ownership and releases phone orientation: initially fullscreen=%s", async active => {
  const fixture = phone(active);
  try {
    await fixture.session.requestFullscreen();
    await fixture.session.restore();
    expect(fixture.unlock).toHaveBeenCalledOnce();
    expect(fixture.exit).toHaveBeenCalledTimes(active ? 0 : 1);
    expect(fixture.fullscreen()).toBe(active ? document.documentElement : null);
    await fixture.session.requestFullscreen();
    expect(fixture.lock).toHaveBeenCalledOnce();
  } finally {fixture.restore();}
});

it("undoes a fullscreen request that resolves after exit without applying a late landscape lock", async () => {
  const fixture = phone();
  const enter = fixture.request.getMockImplementation()!;
  let finish!: () => void;
  fixture.request.mockImplementation(async () => {await new Promise<void>(resolve => {finish = resolve;}); await enter();});
  try {
    const pending = fixture.session.requestFullscreen();
    await fixture.session.restore();
    finish();
    await expect(pending).resolves.toBeNull();
    expect(fixture.lock).not.toHaveBeenCalled();
    expect(fixture.fullscreen()).toBeNull();
    expect(fixture.exit).toHaveBeenCalledOnce();
  } finally {fixture.restore();}
});

it("unlocks an outstanding orientation request without waiting for its timeout", async () => {
  const fixture = phone();
  let finish!: () => void;
  fixture.lock.mockImplementation(() => new Promise<void>(resolve => {finish = resolve;}));
  try {
    const pending = fixture.session.requestFullscreen();
    await Promise.resolve();
    await fixture.session.restore();
    expect(fixture.unlock).toHaveBeenCalledOnce();
    expect(fixture.fullscreen()).toBeNull();
    finish();
    await expect(pending).resolves.toBeNull();
    expect(fixture.lock).toHaveBeenCalledOnce();
  } finally {fixture.restore();}
});


it.each([false, true])("does not let old cleanup exit the next game: late fullscreen first=%s", async lateFirst => {
  const fixture = phone();
  const enter = fixture.request.getMockImplementation()!;
  let finish!: () => void;
  fixture.request.mockImplementationOnce(async () => {await new Promise<void>(resolve => {finish = resolve;}); await enter();});
  try {
    const oldRequest = fixture.session.requestFullscreen();
    await fixture.session.restore();
    const next = beginPlayerPresentation();
    if (lateFirst) {finish(); await oldRequest;}
    await next.requestFullscreen();
    if (!lateFirst) {finish(); await oldRequest;}
    await fixture.session.restore();
    expect(fixture.fullscreen()).toBe(document.documentElement);
    expect(fixture.unlock).not.toHaveBeenCalled();
    await next.restore();
    expect(fixture.fullscreen()).toBeNull();
    expect(fixture.unlock).toHaveBeenCalledOnce();
  } finally {fixture.restore();}
});
