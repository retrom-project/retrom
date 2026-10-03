import {useLayoutEffect, useRef, type RefObject} from "react";
import type {ImmersivePlayerOverlay} from "./use-immersive-player";

const enabledButtons = "button:not(:disabled)";

export function useImmersiveMenuFocus(overlay: ImmersivePlayerOverlay, returnTarget?: RefObject<HTMLElement | null>) {
  const dialog = useRef<HTMLElement>(null);
  const previous = useRef<HTMLElement | null>(null);
  const open = overlay.kind !== "closed";
  const selected = overlay.kind === "menu" ? overlay.selected : null;
  const pending = overlay.kind === "menu" && overlay.pending;
  useLayoutEffect(() => {
    if (open) {
      previous.current ??= document.activeElement instanceof HTMLElement ? document.activeElement : null;
      return;
    }
    if (!previous.current) {return;}
    const stage = returnTarget?.current;
    const target = stage?.querySelector<HTMLElement>("iframe, canvas[tabindex]") ?? stage ?? previous.current;
    previous.current = null;
    if (target.isConnected) {target.focus({preventScroll: true});}
  }, [open, returnTarget]);
  useLayoutEffect(() => {
    const container = dialog.current;
    if (!container) {return;}
    const focus = () => {
      const target = container.querySelector<HTMLElement>(`${enabledButtons}[aria-current="true"]`) ??
        container.querySelector<HTMLElement>(enabledButtons) ?? container;
      if (document.activeElement !== target) {target.focus({preventScroll: true});}
    };
    const contain = (event: FocusEvent) => {
      if (event.target instanceof Node && !container.contains(event.target)) {focus();}
    };
    const tab = (event: KeyboardEvent) => {
      if (event.key !== "Tab") {return;}
      event.preventDefault();
      const buttons = Array.from(container.querySelectorAll<HTMLElement>(enabledButtons));
      const index = buttons.indexOf(document.activeElement as HTMLElement);
      const next = index < 0 ? (event.shiftKey ? buttons.length - 1 : 0) :
        (index + (event.shiftKey ? -1 : 1) + buttons.length) % buttons.length;
      (buttons[next] ?? container).focus({preventScroll: true});
    };
    // A core can focus its own iframe without a focus event reaching this document.
    let timer: number | undefined;
    const onBlur = () => {
      timer = window.setTimeout(() => {
        if (document.hasFocus() && document.activeElement instanceof HTMLIFrameElement) {focus();}
      }, 0);
    };
    focus();
    document.addEventListener("focusin", contain);
    container.addEventListener("keydown", tab);
    window.addEventListener("blur", onBlur);
    return () => {
      document.removeEventListener("focusin", contain);
      container.removeEventListener("keydown", tab);
      window.removeEventListener("blur", onBlur);
      window.clearTimeout(timer);
    };
  }, [overlay.kind, pending, selected]);
  return dialog;
}
