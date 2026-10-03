"use client";

export function PlayerStartupRetry({ onRetry }: { onRetry: () => void }) {
  return <div className="launch-actions">
    <button type="button" className="button" onClick={onRetry}>重试启动</button>
  </div>;
}
