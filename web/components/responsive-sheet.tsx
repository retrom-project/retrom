"use client";

import {
  useEffect,
  useId,
  useRef,
  type ReactNode,
  type RefObject,
} from "react";
import { useModalFocus } from "./modal-focus";
import { AppIcon } from "@/components/app-icon";

export type ResponsiveSheetPlacement = "bottom" | "left" | "right" | "fullscreen";

export function ResponsiveSheet({
  open,
  busy = false,
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
  busy?: boolean;
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

  useModalFocus({ open, locked: busy, panel: panelRef, initial: initialFocusRef, returnTo: returnFocusRef, onCancel: onClose });

  useEffect(() => {
    if (!open) {return;}
    const body = document.body;
    const previousOverflow = body.style.overflow;
    const previousPaddingRight = body.style.paddingRight;
    const scrollbarWidth = window.innerWidth - document.documentElement.clientWidth;
    body.style.overflow = "hidden";
    if (scrollbarWidth > 0) {body.style.paddingRight = `${scrollbarWidth}px`;}
    return () => {
      body.style.overflow = previousOverflow;
      body.style.paddingRight = previousPaddingRight;
    };
  }, [open]);

  if (!open) {return null;}

  return <div className="responsive-sheet-layer">
    <button className="responsive-sheet-backdrop" type="button" tabIndex={-1} aria-label={`关闭${title}`} disabled={busy} onClick={onClose} />
    <aside
      ref={panelRef}
      className={`responsive-sheet responsive-sheet-${placement}${className ? ` ${className}` : ""}`}
      role="dialog"
      aria-modal="true"
      aria-busy={busy}
      aria-label={ariaLabel}
      aria-labelledby={ariaLabel ? undefined : titleId}
      aria-describedby={description ? descriptionId : undefined}
      tabIndex={-1}
    >
      <header className="responsive-sheet-head">
        <div><h2 id={titleId}>{title}</h2>{description ? <p id={descriptionId}>{description}</p> : null}</div>
        <button className="responsive-sheet-close" type="button" aria-label={`关闭${title}`} disabled={busy} onClick={onClose}><AppIcon name="x" /></button>
      </header>
      <div className="responsive-sheet-body">{children}</div>
      {footer ? <footer className="responsive-sheet-footer">{footer}</footer> : null}
    </aside>
  </div>;
}
