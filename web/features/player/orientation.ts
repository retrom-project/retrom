export const mobilePlayerQuery = "(max-width: 1279px), (hover: none) and (pointer: coarse)";
export const portraitPlayerQuery = "(orientation: portrait)";
export const orientationStabilityMs = 250;

export type PlayerOrientationPhase = "player-config" | "orientation-blocked" | "preflight" | "running" | "exiting";
export type PlayerRuntimeKind = "single" | "netplay-p1" | "netplay-p2";
export type PlayerOrientationEffect =
  | "release-input"
  | "pause-single"
  | "pause-netplay"
  | "warn-netplay-p2"
  | "resume-single"
  | "resume-netplay"
  | "unlock";

export type PlayerOrientationState = {
  phase: PlayerOrientationPhase;
  mobile: boolean;
  portrait: boolean;
  hidden: boolean;
  started: boolean;
  runtimeKind: PlayerRuntimeKind;
  pausedBeforeBlock: boolean;
  netplayPauseOwned: boolean;
};

export type PlayerOrientationAction =
  | { type: "config-ready"; mobile: boolean; portrait: boolean; runtimeKind: PlayerRuntimeKind }
  | { type: "runtime-started"; paused: boolean }
  | { type: "orientation-stable"; portrait: boolean; paused: boolean }
  | { type: "netplay-pause-owned" }
  | { type: "visibility"; hidden: boolean }
  | { type: "exit" };

export type PlayerOrientationTransition = {
  state: PlayerOrientationState;
  effects: PlayerOrientationEffect[];
};

export const initialPlayerOrientationState: PlayerOrientationState = {
  phase: "player-config",
  mobile: false,
  portrait: false,
  hidden: false,
  started: false,
  runtimeKind: "single",
  pausedBeforeBlock: false,
  netplayPauseOwned: false,
};

function resumeFromBlock(state: PlayerOrientationState): PlayerOrientationTransition {
  if (!state.started) {return { state: { ...state, phase: "preflight" }, effects: [] };}
  if (state.hidden) {return { state, effects: [] };}
  if (state.runtimeKind === "single" && !state.pausedBeforeBlock) {
    return { state: { ...state, phase: "running" }, effects: ["resume-single"] };
  }
  if (state.runtimeKind === "netplay-p1" && state.netplayPauseOwned) {
    return { state: { ...state, phase: "running", netplayPauseOwned: false }, effects: ["resume-netplay"] };
  }
  return { state: { ...state, phase: "running" }, effects: [] };
}

export function reducePlayerOrientation(state: PlayerOrientationState, action: PlayerOrientationAction): PlayerOrientationTransition {
  switch (action.type) {
    case "config-ready": return reduceConfigReady(state, action);
    case "runtime-started":
      return { state: { ...state, started: true, pausedBeforeBlock: action.paused, phase: "running" }, effects: [] };
    case "exit":
      return { state: { ...state, phase: "exiting" }, effects: state.mobile ? ["unlock"] : [] };
    case "netplay-pause-owned": return reduceNetplayPauseOwned(state);
    case "visibility": return reduceVisibility(state, action.hidden);
    case "orientation-stable": return reduceOrientationStable(state, action);
  }
}

function reduceConfigReady(
  state: PlayerOrientationState,
  action: Extract<PlayerOrientationAction, { type: "config-ready" }>,
): PlayerOrientationTransition {
  const phase = action.mobile && action.portrait ? "orientation-blocked" as const : "preflight" as const;
  return { state: { ...state, mobile: action.mobile, portrait: action.portrait, runtimeKind: action.runtimeKind, phase }, effects: [] };
}

function reduceNetplayPauseOwned(state: PlayerOrientationState): PlayerOrientationTransition {
  const next = { ...state, netplayPauseOwned: true };
  if (!next.portrait && !next.hidden && next.started) {
    return { state: { ...next, phase: "running", netplayPauseOwned: false }, effects: ["resume-netplay"] };
  }
  return { state: next, effects: [] };
}

function reduceVisibility(state: PlayerOrientationState, hidden: boolean): PlayerOrientationTransition {
  const next = { ...state, hidden };
  if (!hidden && next.mobile && !next.portrait && next.phase === "orientation-blocked") {
    return resumeFromBlock(next);
  }
  return { state: next, effects: [] };
}

function reduceOrientationStable(
  state: PlayerOrientationState,
  action: Extract<PlayerOrientationAction, { type: "orientation-stable" }>,
): PlayerOrientationTransition {
  if (!state.mobile || state.phase === "exiting") {
    return { state: { ...state, portrait: action.portrait }, effects: [] };
  }
  if (action.portrait) {
    if (state.portrait && state.phase === "orientation-blocked") {return { state, effects: [] };}
    const next = {
      ...state,
      portrait: true,
      phase: "orientation-blocked" as const,
      pausedBeforeBlock: state.started ? action.paused : state.pausedBeforeBlock,
      netplayPauseOwned: false,
    };
    if (!state.started) {return { state: next, effects: [] };}
    const effects: PlayerOrientationEffect[] = ["release-input"];
    if (state.runtimeKind === "single" && !action.paused) {effects.push("pause-single");}
    if (state.runtimeKind === "netplay-p1" && !action.paused) {effects.push("pause-netplay");}
    if (state.runtimeKind === "netplay-p2") {effects.push("warn-netplay-p2");}
    return { state: next, effects };
  }
  const next = { ...state, portrait: false };
  if (state.phase !== "orientation-blocked") {return { state: next, effects: [] };}
  return resumeFromBlock(next);
}

export function observeStableOrientation(query: MediaQueryList, callback: (portrait: boolean) => void, delayMs = orientationStabilityMs) {
  let timer: number | undefined;
  const schedule = () => {
    if (timer !== undefined) {window.clearTimeout(timer);}
    const expected = query.matches;
    timer = window.setTimeout(() => {
      timer = undefined;
      if (query.matches === expected) {callback(expected);}
    }, delayMs);
  };
  query.addEventListener("change", schedule);
  window.addEventListener("orientationchange", schedule);
  return () => {
    if (timer !== undefined) {window.clearTimeout(timer);}
    query.removeEventListener("change", schedule);
    window.removeEventListener("orientationchange", schedule);
  };
}

export function waitForStableLandscape(query: MediaQueryList, signal: AbortSignal, onPortrait: (portrait: boolean) => void) {
  if (!query.matches) {return Promise.resolve();}
  onPortrait(true);
  return new Promise<void>((resolve, reject) => {
    const stop = observeStableOrientation(query, (portrait) => {
      onPortrait(portrait);
      if (!portrait) {
        stop();
        signal.removeEventListener("abort", abort);
        resolve();
      }
    });
    const abort = () => {
      stop();
      reject(new DOMException("Player orientation wait aborted", "AbortError"));
    };
    signal.addEventListener("abort", abort, { once: true });
  });
}

type LockableOrientation = ScreenOrientation & {
  lock?: (orientation: "landscape") => Promise<void>;
  unlock?: () => void;
};

export type LandscapeRequestResult = {
  fullscreen: "active" | "denied";
  orientation: "locked" | "unsupported" | "denied";
};

export async function requestFullscreenAndLandscape(root: HTMLElement = document.documentElement): Promise<LandscapeRequestResult> {
  const fullscreenDocument = root.ownerDocument;
  const fullscreenRequest = root.requestFullscreen({ navigationUI: "hide" })
    .then(() => "active" as const)
    .catch(() => fullscreenDocument.fullscreenElement ? "active" as const : "denied" as const);
  const fullscreen = await fullscreenRequest;
  const orientation = screen.orientation as LockableOrientation | undefined;
  if (typeof orientation?.lock !== "function") {return { fullscreen, orientation: "unsupported" };}
  const locked = await orientation.lock("landscape").then(() => "locked" as const).catch(() => "denied" as const);
  return { fullscreen, orientation: locked };
}

export function unlockLandscape() {
  const orientation = screen.orientation as LockableOrientation | undefined;
  try { orientation?.unlock?.(); } catch { /* best effort; exiting must continue */ }
}
