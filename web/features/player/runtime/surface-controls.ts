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
    if (options.experience === "immersive") {
      if (event.key.toLowerCase() !== "m") {return;}
      event.preventDefault();
      event.stopImmediatePropagation();
      options.onImmersiveMenuShortcut();
      return;
    }
    if (shouldRevealPlayerControlsForKey(event.key)) {options.onShowControls();}
    if (!isPauseShortcut(event)) {return;}
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

function isPauseShortcut(event: KeyboardEvent) {
  if (event.code !== "KeyP" || event.repeat || event.isComposing ||
    event.ctrlKey || event.altKey || event.metaKey) {return false;}
  const target = event.target as {closest?: (selectors: string) => Element | null} | null;
  return !(typeof target?.closest === "function" &&
    target.closest("input,select,textarea,[contenteditable=true]"));
}
