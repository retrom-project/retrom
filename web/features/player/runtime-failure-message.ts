import type { RuntimeFailureV1 } from "./runtime/contract";

const summaries: Record<RuntimeFailureV1["category"], string> = {
  CONTENT: "核心无法加载游戏文件，请检查游戏内容和运行配置。",
  NETWORK: "游戏资源加载失败，请检查网络连接后重试。",
  STORAGE: "浏览器存储不可用，请检查存储空间和权限后重试。",
  SECURITY: "浏览器阻止了游戏运行，请检查浏览器权限。",
  CONFIGURATION: "运行配置不可用，请检查核心和游戏配置。",
  CORE: "运行核心发生错误，请返回后重试。",
};

export function runtimeFailureMessage(failure: RuntimeFailureV1): string {
  const summary = failure.code === "PLAYER_CONTENT_ENCRYPTED"
    ? "核心无法加载当前加密内容，请检查游戏文件和运行配置。"
    : summaries[failure.category];
  // CORE is the Provider's sanitized native evidence. Runtime codes and stacks
  // remain available in the diagnostic event for debugging.
  const native = failure.diagnostics.filter(item => item.source === "CORE").map(item => item.message);
  return [summary, ...native].join("\n");
}
