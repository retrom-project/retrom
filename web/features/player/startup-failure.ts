type StartupFailure = { message: string; retryable: boolean; stage: "config" | "runtime" };
const runtimeFailure = (message: string): StartupFailure => ({ message, retryable: true, stage: "runtime" });
const configFailure = (message: string, retryable: boolean): StartupFailure => ({ message, retryable, stage: "config" });

const startupFailures: Record<string, StartupFailure> = {
  PLAYER_LAUNCH_SERVICE_UNAVAILABLE: configFailure("服务器暂时无法提供启动配置，请稍后重试。", true),
  PLAYER_LAUNCH_NETWORK_FAILED: configFailure("无法连接服务器读取启动配置，请检查网络后重试。", true),
  PLAYER_LAUNCH_SESSION_EXPIRED: configFailure("本次启动凭据已失效，请返回游戏页面重新启动。", false),
  PLAYER_LAUNCH_UNAVAILABLE: configFailure("本次启动已不可用，请返回游戏页面重新启动。", false),
  PLAYER_LAUNCH_CONFIG_INVALID: configFailure("启动配置无效，请联系管理员检查运行配置。", false),
  PLAYER_RESOURCE_IDLE_TIMEOUT: runtimeFailure("资源下载已停止推进，请检查网络后重试。"),
  PLAYER_RESOURCE_NETWORK_FAILED: runtimeFailure("资源下载失败，请检查网络后重试。"),
  PLAYER_CORE_INITIALIZATION_TIMEOUT: runtimeFailure("资源已就绪，但核心初始化超时。请重试启动。"),
  PLAYER_RUNTIME_INITIALIZATION_FAILED: runtimeFailure("核心初始化失败，请重试启动或联系管理员检查运行依赖。"),
  PLAYER_RUNTIME_CSP_BLOCKED: runtimeFailure("运行资源被浏览器安全策略阻止，请联系管理员更新运行依赖。"),
};

export function startupFailure(code: string): StartupFailure {
  return startupFailures[code] ?? { message: code, retryable: false, stage: "runtime" };
}
