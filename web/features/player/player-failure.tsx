import Link from "next/link";
import {playerReturnLabel, type PlayerReturnIntent} from "@/lib/navigation/player-return";
import {PlayerStartupRetry} from "./player-startup-retry";
import type {RuntimeFailureV1} from "./runtime/contract";

const guidance: Record<RuntimeFailureV1["category"], string> = {
  CONTENT: "游戏内容不符合当前核心要求。请根据诊断修正内容，或联系管理员检查兼容性。",
  NETWORK: "运行资源下载中断，请检查网络后重试。",
  STORAGE: "浏览器存储不可用，请检查存储权限和可用空间后重新进入游戏。",
  SECURITY: "运行资源被浏览器安全策略阻止，请联系管理员更新运行依赖。",
  CORE: "游戏核心意外停止，请将错误码和诊断信息提供给管理员排查。",
  CONFIGURATION: "运行配置与当前核心契约不一致，请联系管理员检查运行依赖。",
};

export function PlayerFailure({failure, returnIntent, onRetry}: {
  failure: RuntimeFailureV1; returnIntent: PlayerReturnIntent; onRetry: () => void;
}) {
  return <section className="player-loading player-failure" role="alert">
    <strong>{failure.phase === "PLAYING" ? "游戏运行中断" : "游戏启动失败"}</strong>
    <p>{failure.code === "PLAYER_CONTENT_ENCRYPTED" ? "该镜像仍处于加密状态，请提供已解密的兼容 3DS 镜像。" : guidance[failure.category]}</p>
    <code>{failure.code}</code>
    {failure.diagnostics.length ? <details className="player-failure-details" open>
      <summary>诊断详情</summary>
      <div className="player-failure-log" tabIndex={0} aria-label="核心诊断文本">{failure.diagnostics.map((item, index) => <pre key={index}>{item.message}</pre>)}</div>
    </details> : null}
    {failure.retryable ? <PlayerStartupRetry onRetry={onRetry} /> : null}
    <Link href={returnIntent.href}>{playerReturnLabel(returnIntent)}</Link>
  </section>;
}
