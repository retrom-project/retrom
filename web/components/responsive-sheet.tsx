"use client";

import {
  useEffect,
  useId,
  useRef,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  type RefObject,
} from "react";
import { AppIcon } from "@/components/app-icon";

const focusableSelector = [
  "a[href]",
  "button:not(:disabled)",
  "input:not(:disabled)",
  "select:not(:disabled)",
  "textarea:not(:disabled)",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

export type ResponsiveSheetPlacement = "bottom" | "left" | "right" | "fullscreen";

export function ResponsiveSheet({
  open,
  title,
  description,
  placement = "bottom",
  onClose,
  returnFocusRef,
  initialFocusRef,
  children,
  footer,
  className = "",
  ariaLabel,
}: {
  open: boolean;
  title: string;
  description?: string;
  placement?: ResponsiveSheetPlacement;
  onClose: () => void;
  returnFocusRef?: RefObject<HTMLElement | null>;
  initialFocusRef?: RefObject<HTMLElement | null>;
  children: ReactNode;
  footer?: ReactNode;
  className?: string;
  ariaLabel?: string;
}) {
  const titleId = useId();
  const descriptionId = useId();
  const panelRef = useRef<HTMLElement>(null);

  useEffect(() => {
    if (!open) {return;}
    const body = document.body;
    const previousOverflow = body.style.overflow;
    const previousPaddingRight = body.style.paddingRight;
    const returnFocus = returnFocusRef?.current;
    const scrollbarWidth = window.innerWidth - document.documentElement.clientWidth;
    body.style.overflow = "hidden";
    if (scrollbarWidth > 0) {body.style.paddingRight = `${scrollbarWidth}px`;}
    const frame = window.requestAnimationFrame(() => {
      (initialFocusRef?.current ?? panelRef.current?.querySelector<HTMLElement>(focusableSelector))?.focus();
    });
    return () => {
      window.cancelAnimationFrame(frame);
      body.style.overflow = previousOverflow;
      body.style.paddingRight = previousPaddingRight;
      returnFocus?.focus();
    };
  }, [initialFocusRef, open, returnFocusRef]);

  if (!open) {return null;}

  function trapFocus(event: ReactKeyboardEvent<HTMLElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      onClose();
      return;
    }
    if (event.key !== "Tab") {return;}
    const focusable = Array.from(panelRef.current?.querySelectorAll<HTMLElement>(focusableSelector) ?? [])
      .filter((element) => !element.hidden && element.getAttribute("aria-hidden") !== "true");
    if (!focusable.length) {
      event.preventDefault();
      panelRef.current?.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable.at(-1);
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first?.focus();
    }
  }

  return <div className="responsive-sheet-layer">
    <button className="responsive-sheet-backdrop" type="button" tabIndex={-1} aria-label={`关闭${title}`} onClick={onClose} />
    <aside
      ref={panelRef}
      className={`responsive-sheet responsive-sheet-${placement}${className ? ` ${className}` : ""}`}
      role="dialog"
      aria-modal="true"
      aria-label={ariaLabel}
      aria-labelledby={ariaLabel ? undefined : titleId}
      aria-describedby={description ? descriptionId : undefined}
      tabIndex={-1}
      onKeyDown={trapFocus}
    >
      <header className="responsive-sheet-head">
        <div><h2 id={titleId}>{title}</h2>{description ? <p id={descriptionId}>{description}</p> : null}</div>
        <button className="responsive-sheet-close" type="button" aria-label={`关闭${title}`} onClick={onClose}><AppIcon name="x" /></button>
      </header>
      <div className="responsive-sheet-body">{children}</div>
      {footer ? <footer className="responsive-sheet-footer">{footer}</footer> : null}
    </aside>
  </div>;
}
