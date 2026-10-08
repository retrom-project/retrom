"use client";
export default function ErrorPage({ reset }: { reset: () => void }) {
  return (
    <div className="empty" role="alert">
      <h1>页面加载失败</h1>
      <button className="button" onClick={reset}>
        重试
      </button>
    </div>
  );
}
