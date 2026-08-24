"use client";

import { useEffect, useRef } from "react";
import styles from "./immersive.module.css";

export type Choice = Readonly<{ id: string; label: string; tone?: "danger" }>;

export function ImmersiveChoiceDialog({ choices, description, onChoose, selectedId, title }: {
  choices: readonly Choice[];
  description: string;
  onChoose: (id: string) => void;
  selectedId: string;
  title: string;
}) {
  const selectedRef = useRef<HTMLButtonElement>(null);
  useEffect(() => selectedRef.current?.focus(), [selectedId]);
  return <div className={styles.dialogBackdrop}>
    <section className={styles.choiceDialog} role="alertdialog" aria-modal="true" aria-labelledby="immersive-dialog-title" aria-describedby="immersive-dialog-description">
      <span className={styles.dialogMark} aria-hidden="true">R</span>
      <h2 id="immersive-dialog-title">{title}</h2>
      <p id="immersive-dialog-description">{description}</p>
      <div className={styles.dialogActions}>
        {choices.map((choice) => <button
          ref={choice.id === selectedId ? selectedRef : undefined}
          className={`${styles.dialogButton} ${choice.id === selectedId ? styles.selected : ""} ${choice.tone === "danger" ? styles.danger : ""}`.trim()}
          type="button"
          key={choice.id}
          onClick={() => onChoose(choice.id)}
        >{choice.label}</button>)}
      </div>
      <small>A 确认 · B 取消 · 左右选择</small>
    </section>
  </div>;
}
