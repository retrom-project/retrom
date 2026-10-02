"use client";

export function PlayerStartupRetry() {
  return <div className="launch-actions">
    <button type="button" className="button" onClick={() => window.location.reload()}>重试启动</button>
  </div>;
}
