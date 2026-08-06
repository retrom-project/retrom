"use client";

export default function ErrorPage({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <section className="empty page-error"><span aria-hidden="true">!</span><h1>暂时无法读取数据</h1><p>后端服务可能仍在启动，请稍后重试。</p><button className="button" onClick={reset}>重新加载</button></section>;
}
