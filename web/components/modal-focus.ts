"use client";

import { useEffectEvent, useLayoutEffect, useRef, type RefObject } from "react";

const layers: HTMLElement[] = [];
const focusableSelector =
  "button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex='-1'])";

function ownedPanels(panel: HTMLElement) {
  const controlled = Array.from(
    panel.querySelectorAll<HTMLElement>(
      '[aria-expanded="true"][aria-controls]',
    ),
  )
    .flatMap((element) =>
      (element.getAttribute("aria-controls") ?? "").split(/\s+/),
    )
    .map((id) => document.getElementById(id))
    .filter(
      (element): element is HTMLElement =>
        element !== null && !panel.contains(element),
    );
  return [panel, ...controlled];
}

function focusable(panel: HTMLElement) {
  return ownedPanels(panel)
    .flatMap((owner) =>
      Array.from(owner.querySelectorAll<HTMLElement>(focusableSelector)),
    )
    .filter(
      (element) =>
        !element.closest("[hidden], [inert], [aria-hidden='true']") &&
        getComputedStyle(element).display !== "none" &&
        getComputedStyle(element).visibility !== "hidden",
    )
    .sort((left, right) =>
      left === right
        ? 0
        : left.compareDocumentPosition(right) & Node.DOCUMENT_POSITION_FOLLOWING
          ? -1
          : 1,
    );
}

function childOwnsEscape(panel: HTMLElement, target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  return (
    target.matches('[role="combobox"][aria-expanded="true"][aria-controls]') ||
    ownedPanels(panel)
      .slice(1)
      .some((owner) => owner.contains(target))
  );
}

type Options = {
  open: boolean;
  locked: boolean;
  panel: RefObject<HTMLElement | null>;
  initial?: RefObject<HTMLElement | null>;
  returnTo?: RefObject<HTMLElement | null>;
  onCancel: () => void;
};

export function useModalFocus({
  open,
  locked,
  panel,
  initial,
  returnTo,
  onCancel,
}: Options) {
  const lastFocused = useRef<HTMLElement | null>(null);
  const restoreFocus = useEffectEvent(() => {
    const node = panel.current;
    if (!node) {
      return;
    }
    const choices = focusable(node);
    const preferred = lastFocused.current ?? initial?.current;
    (locked
      ? node
      : preferred && choices.includes(preferred)
        ? preferred
        : (choices[0] ?? node)
    ).focus();
  });
  const onFocus = useEffectEvent((event: FocusEvent) => {
    const node = panel.current;
    if (!node || layers.at(-1) !== node) {
      return;
    }
    const target = event.target;
    if (
      !(target instanceof HTMLElement) ||
      !ownedPanels(node).some((owner) => owner.contains(target))
    ) {
      restoreFocus();
      return;
    }
    if (target !== node && !locked) {
      lastFocused.current = target;
    }
  });
  const onKey = useEffectEvent((event: KeyboardEvent) => {
    const node = panel.current;
    if (!node || layers.at(-1) !== node) {
      return;
    }
    if (event.key === "Escape") {
      if (childOwnsEscape(node, event.target)) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      if (!locked) {
        onCancel();
      }
      return;
    }
    if (event.key !== "Tab") {
      return;
    }
    const choices = locked ? [] : focusable(node);
    const current = document.activeElement;
    const first = choices[0];
    const last = choices.at(-1);
    if (!first || !last) {
      event.preventDefault();
      node.focus();
      return;
    }
    if (
      !choices.includes(current as HTMLElement) ||
      (event.shiftKey ? current === first : current === last)
    ) {
      event.preventDefault();
      (event.shiftKey ? last : first).focus();
    }
  });

  useLayoutEffect(() => {
    const node = panel.current;
    if (!open || !node) {
      return;
    }
    const previous =
      returnTo?.current ??
      (document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null);
    lastFocused.current = null;
    layers.push(node);
    const focus = (event: FocusEvent) => onFocus(event);
    const key = (event: KeyboardEvent) => onKey(event);
    document.addEventListener("focusin", focus, true);
    document.addEventListener("keydown", key, true);
    restoreFocus();
    return () => {
      document.removeEventListener("focusin", focus, true);
      document.removeEventListener("keydown", key, true);
      const wasTop = layers.at(-1) === node;
      const index = layers.indexOf(node);
      if (index >= 0) {
        layers.splice(index, 1);
      }
      if (wasTop && previous?.isConnected) {
        previous.focus();
      }
    };
  }, [open, panel, returnTo]);

  useLayoutEffect(() => {
    if (open && layers.at(-1) === panel.current) {
      restoreFocus();
    }
  }, [locked, open, panel]);
}
