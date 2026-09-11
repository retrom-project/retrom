"use client";

import {useEffect, useId, useRef, useState} from "react";
import {createPortal} from "react-dom";

const replacementHint = "替换会清理依赖旧 BIOS 的存档与运行会话";

export function BIOSFileButton({installed, attention, busy, disabled, onClick}: {
  installed: boolean; attention: boolean; busy: boolean; disabled: boolean; onClick: () => void;
}) {
  const id = useId();
  const button = useRef<HTMLButtonElement>(null);
  const [position, setPosition] = useState<{left: number; top: number; below: boolean} | null>(null);
  const visible = installed && !disabled && position !== null;
  useEffect(() => {
    if (!visible) {return;}
    const dismiss = () => setPosition(null);
    window.addEventListener("scroll", dismiss, true);
    window.addEventListener("resize", dismiss);
    return () => {
      window.removeEventListener("scroll", dismiss, true);
      window.removeEventListener("resize", dismiss);
    };
  }, [visible]);

  function showHint() {
    if (!installed || disabled || !button.current) {return;}
    const rect = button.current.getBoundingClientRect();
    const halfWidth = Math.min(280, window.innerWidth - 24) / 2;
    setPosition({left: Math.max(halfWidth + 12, Math.min(rect.left + rect.width / 2, window.innerWidth - halfWidth - 12)), top: rect.top < 80 ? rect.bottom + 8 : rect.top - 8, below: rect.top < 80});
  }

  return <>
    <button ref={button} className={`button ${attention ? "" : "secondary"} compact`} type="button" disabled={disabled}
      aria-describedby={installed ? id : undefined} onClick={onClick} onPointerEnter={showHint} onPointerLeave={() => setPosition(null)}
      onFocus={showHint} onBlur={() => setPosition(null)} onKeyDown={(event) => {if (event.key === "Escape") {setPosition(null);}}}>
      {busy ? "验证中…" : installed ? "替换文件" : "选择 BIOS 文件"}
    </button>
    {installed ? <span id={id} className="sr-only">{replacementHint}</span> : null}
    {visible ? createPortal(<span role="tooltip" className={`runtime-replacement-hint${position.below ? " is-below" : ""}`} style={{left: position.left, top: position.top}}>{replacementHint}</span>, document.body) : null}
  </>;
}
