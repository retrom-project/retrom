"use client";

import Link from "next/link";

export default function NetplayRoomError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <section className="empty page-error"><span aria-hidden="true">!</span><h1>无法同步联机房间</h1><p>房间可能已经结束，或实时状态暂时不可用。</p><div className="empty-actions"><button className="button" type="button" onClick={reset}>重试</button><Link className="button secondary" href="/netplay">返回联机首页</Link></div></section>;
}
