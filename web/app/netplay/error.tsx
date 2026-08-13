"use client";

export default function NetplayError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <section className="empty page-error"><span aria-hidden="true">!</span><h1>暂时无法读取联机房间</h1><p>房间服务可能正在恢复，请稍后重试。</p><button className="button" type="button" onClick={reset}>重新加载</button></section>;
}
