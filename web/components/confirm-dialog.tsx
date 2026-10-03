"use client";

import { useId, useRef, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useModalFocus } from "./modal-focus";

type DialogTone = "default" | "danger";

type ConfirmDialogProps = {
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
  role?: "alertdialog" | "dialog";
  dialogClassName?: string;
  interactionDisabled?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
  onLeading?: () => void;
  onSecondary?: () => void;
};

const confirmDialogDefaults = {
  busy: false,
  cancelLabel: "取消",
  confirmDisabled: false,
  confirmLabel: "确认",
  hideCancel: false,
  interactionDisabled: false,
  leadingBusy: false,
  leadingBusyLabel: "处理中…",
  leadingDisabled: false,
  portalToBody: false,
  role: "alertdialog" as const,
  tone: "default" as DialogTone,
  wide: false,
};

export function ConfirmDialog(input: ConfirmDialogProps) {
  const props = { ...confirmDialogDefaults, ...input };
  const {
    busy, cancelLabel, children, confirmDisabled, confirmLabel, description, dialogClassName, hideCancel, interactionDisabled, leadingBusy,
    leadingBusyLabel, leadingDisabled, leadingLabel, onCancel, onConfirm, onLeading, onSecondary, open,
    portalToBody, role, secondaryLabel, title, tone, wide,
  } = props;
  const titleId = useId();
  const descriptionId = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const locked = busy || leadingBusy || interactionDisabled;

  useModalFocus({ open, locked, panel: dialogRef, initial: cancelRef, onCancel });

  if (!open) {return null;}

  const layer = (
    <div
      className="dialog-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !locked) {onCancel();}
      }}
    >
      <section
        ref={dialogRef}
        tabIndex={-1}
        className={`app-dialog ${tone === "danger" ? "is-danger" : ""} ${wide ? "is-wide" : ""} ${dialogClassName ?? ""}`.trim()}
        role={role}
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
      >
        <div className="dialog-copy">
          <span className="dialog-mark" aria-hidden="true">{tone === "danger" ? "!" : "i"}</span>
          <div>
            <h2 id={titleId}>{title}</h2>
            {description ? <p id={descriptionId}>{description}</p> : null}
          </div>
        </div>
        {children ? <div className="dialog-impact">{children}</div> : null}
        <DialogActions
          {...{ busy, cancelLabel, confirmDisabled, confirmLabel, hideCancel, leadingBusy, leadingBusyLabel,
            leadingDisabled, leadingLabel, locked, onCancel, onConfirm, onLeading, onSecondary, secondaryLabel, tone }}
          cancelRef={cancelRef}
        />
      </section>
    </div>
  );
  return portalToBody ? createPortal(layer, document.body) : layer;
}

type DialogActionsProps = Pick<ConfirmDialogProps,
  "busy" | "cancelLabel" | "confirmDisabled" | "confirmLabel" | "hideCancel" | "leadingBusy" |
  "leadingBusyLabel" | "leadingDisabled" | "leadingLabel" | "onCancel" | "onConfirm" | "onLeading" |
  "onSecondary" | "secondaryLabel" | "tone"
> & { cancelRef: RefObject<HTMLButtonElement | null>; locked: boolean };

function DialogActions(props: DialogActionsProps) {
  const {
    busy, cancelLabel, cancelRef, confirmDisabled, confirmLabel, hideCancel, leadingBusy, leadingBusyLabel,
    leadingDisabled, leadingLabel, locked, onCancel, onConfirm, onLeading, onSecondary, secondaryLabel, tone,
  } = props;
  return <div className="dialog-actions">
    {leadingLabel && onLeading ? <button className="button secondary dialog-leading-action" type="button" disabled={locked || leadingDisabled} onClick={onLeading}>{leadingBusy ? leadingBusyLabel : leadingLabel}</button> : null}
    {hideCancel ? null : <button ref={cancelRef} className="button secondary" type="button" disabled={locked} onClick={onCancel}>{cancelLabel}</button>}
    {secondaryLabel && onSecondary ? <button className="button secondary" type="button" disabled={locked} onClick={onSecondary}>{secondaryLabel}</button> : null}
    <button className={`button${tone === "danger" ? " danger" : ""}`} type="button" disabled={locked || confirmDisabled} onClick={onConfirm}>{busy ? "处理中…" : confirmLabel}</button>
  </div>;
}
