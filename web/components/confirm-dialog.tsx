"use client";

import { useEffect, useId, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";

type DialogTone = "default" | "danger";

export function ConfirmDialog({
  open,
  title,
  description,
  children,
  confirmLabel = "确认",
  cancelLabel = "取消",
  leadingLabel,
  leadingBusyLabel = "处理中…",
  secondaryLabel,
  tone = "default",
  busy = false,
  confirmDisabled = false,
  leadingBusy = false,
  leadingDisabled = false,
  wide = false,
  hideCancel = false,
  portalToBody = false,
  onConfirm,
  onCancel,
  onLeading,
  onSecondary,
}: {
  open: boolean;
  title: string;
  description?: string;
  children?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  leadingLabel?: string;
  leadingBusyLabel?: string;
  secondaryLabel?: string;
  tone?: DialogTone;
  busy?: boolean;
  confirmDisabled?: boolean;
  leadingBusy?: boolean;
  leadingDisabled?: boolean;
  wide?: boolean;
  hideCancel?: boolean;
  portalToBody?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
  onLeading?: () => void;
  onSecondary?: () => void;
}) {
  const titleId = useId();
  const descriptionId = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const locked = busy || leadingBusy;

  useEffect(() => {
    if (!open) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    (cancelRef.current ?? dialogRef.current?.querySelector<HTMLElement>("button:not(:disabled)"))?.focus();
    return () => previous?.focus();
  }, [open]);

  if (!open) return null;

  const layer = (
    <div
      className="dialog-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !locked) onCancel();
      }}
    >
      <section
        ref={dialogRef}
        className={`app-dialog ${tone === "danger" ? "is-danger" : ""} ${wide ? "is-wide" : ""}`}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        onKeyDown={(event) => {
          if (event.key === "Escape" && !locked) onCancel();
          if (event.key === "Tab") {
            const focusable = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>("button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex='-1'])") ?? []);
            if (!focusable.length) return;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
            else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
          }
        }}
      >
        <div className="dialog-copy">
          <span className="dialog-mark" aria-hidden="true">{tone === "danger" ? "!" : "i"}</span>
          <div>
            <h2 id={titleId}>{title}</h2>
            {description ? <p id={descriptionId}>{description}</p> : null}
          </div>
        </div>
        {children ? <div className="dialog-impact">{children}</div> : null}
        <div className="dialog-actions">
          {leadingLabel && onLeading ? <button className="button secondary dialog-leading-action" type="button" disabled={locked || leadingDisabled} onClick={onLeading}>{leadingBusy ? leadingBusyLabel : leadingLabel}</button> : null}
          {hideCancel ? null : <button ref={cancelRef} className="button secondary" type="button" disabled={locked} onClick={onCancel}>{cancelLabel}</button>}
          {secondaryLabel && onSecondary ? <button className="button secondary" type="button" disabled={locked} onClick={onSecondary}>{secondaryLabel}</button> : null}
          <button className={`button${tone === "danger" ? " danger" : ""}`} type="button" disabled={locked || confirmDisabled} onClick={onConfirm}>{busy ? "处理中…" : confirmLabel}</button>
        </div>
      </section>
    </div>
  );
  return portalToBody ? createPortal(layer, document.body) : layer;
}
