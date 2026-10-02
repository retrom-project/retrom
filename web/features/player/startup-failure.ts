const startupFailures: Record<string, string> = {
  PLAYER_RESOURCE_IDLE_TIMEOUT: "资源下载已停止推进，请检查网络后重试。",
  PLAYER_RESOURCE_NETWORK_FAILED: "资源下载失败，请检查网络后重试。",
  PLAYER_CORE_INITIALIZATION_TIMEOUT: "资源已就绪，但核心初始化超时。请重试启动。",
  PLAYER_RUNTIME_INITIALIZATION_FAILED: "核心初始化失败，请重试启动或联系管理员检查运行依赖。",
  PLAYER_RUNTIME_CSP_BLOCKED: "运行资源被浏览器安全策略阻止，请联系管理员更新运行依赖。",
};

export function startupFailureMessage(code: string): string | undefined {
  return startupFailures[code];
}
