import {shouldRevealPlayerControlsForKey} from "../player-controls-visibility";
import type {PlayerRuntimeV1} from "./contract";

type RuntimeSurfaceControlOptions = {
  experience: "standard" | "immersive";
  onKeyboardPause: () => void;
  onImmersiveMenuShortcut: () => void;
  onRevealControls: (clientY: number) => void;
  onShowControls: () => void;
  onSurface: () => void;
};

export function installRuntimeSurfaceControls(
  runtime: PlayerRuntimeV1,
  options: RuntimeSurfaceControlOptions,
) {
  const frameDocument = runtime.getCanvas()?.ownerDocument;
  if (!frameDocument) {return () => undefined;}
  const keydown = (event: KeyboardEvent) => {
    const shortcuts = runtime.getInputCapabilities?.().hostShortcuts ?? [];
    if (options.experience === "immersive") {
      if (!shortcuts.includes("MENU") || !isShortcut(event, "KeyM")) {return;}
      event.preventDefault();
      event.stopImmediatePropagation();
      options.onImmersiveMenuShortcut();
      return;
    }
    if (shouldRevealPlayerControlsForKey(event.key)) {options.onShowControls();}
    if (!shortcuts.includes("PAUSE") || !isShortcut(event, "KeyP")) {return;}
    event.preventDefault();
    event.stopImmediatePropagation();
    options.onKeyboardPause();
  };
  const pointermove = (event: PointerEvent) => options.onRevealControls(event.clientY);
  const click = (event: MouseEvent) => {
    const target = event.target;
    if (target instanceof frameDocument.defaultView!.Element &&
      target.closest("button,a,input,select,textarea,[contenteditable=true],[role=button]")) {return;}
    options.onSurface();
  };
  frameDocument.addEventListener("keydown", keydown);
  if (options.experience === "standard") {
    frameDocument.addEventListener("pointermove", pointermove, {passive: true});
    frameDocument.addEventListener("click", click);
  }
  return () => {
    frameDocument.removeEventListener("keydown", keydown);
    frameDocument.removeEventListener("pointermove", pointermove);
    frameDocument.removeEventListener("click", click);
  };
}

function isShortcut(event: KeyboardEvent, code: "KeyP" | "KeyM") {
  if (event.code !== code || event.repeat || event.isComposing ||
    event.ctrlKey || event.altKey || event.metaKey) {return false;}
  const target = event.target as {closest?: (selectors: string) => Element | null} | null;
  return !(typeof target?.closest === "function" &&
    target.closest("input,select,textarea,[contenteditable=true]"));
}
