# 核心资源缓存验证

2026-10-14，`core-asset-cache` PFB 完成核心资源持久缓存与测试发现问题的通用修复。没有数据库表、索引、迁移或特定 ROM 兼容分支；原始游戏和 BIOS 文件保持不变。

## 行为与范围

- EmulatorJS 两个版本及原生核心通过统一 Content I/O 读取经过摘要、长度校验的核心归档、Wasm、data 和 Worker 资源。已下载脚本实际从校验后的字节导入，保留正确的相对依赖目录。
- 不可变小型 Provider 启动资源使用私有 HTTP 缓存；大型二进制由 Content I/O 持久缓存。所有资源保留 `no-transform`，避免代理改写字节和 Range。
- PFB 覆盖 URL 包含模块摘要；旧摘要返回 404。大资源按摘要保存到文件，描述不再内联全部二进制，宿主按文件校验并流式提供资源。
- 下载失败、取消、退出释放租约及 Blob URL；损坏缓存可重新下载。EmulatorJS 的所有退出路径停止核心，保留保存和卸载，退出时不再重新启动核心。
- ONS 在有效内容读取期间不误报启动超时，退出时先终止异步核心；PV-1000 按卡带实际容量加载，仅补齐模拟内存；Supermodel 使用核心真实支持的 ZIP，通用导入保留核心声明的原生归档。

## 已有证据

候选矩阵有 128 个游戏用例，127 个完成导入，覆盖 109 / 110 个产品 Target。120 个具有大型核心资产的用例在普通 HTTP 缓存关闭时，热启动大型核心请求为 0；另 7 个脚本或游戏自带引擎用例不计作大型资产缓存证明。EmulatorJS PPSSPP 无产品入口，另外完成组件级真实 ISO 冷/热测试，不能当作产品验收。

真实问题回归包括：FBAlpha 同核心换游戏、跨浏览器重启缓存、损坏后重试，以及 Captain Commando 输入/保存/新实例恢复；PV-1000 Ascend、Coleco Neuras、SG-1000 Air Battle 与另外 6 个 MAME 平台；ONS《银色》的语言选择、剧情输入、取消重试和保存恢复；Supermodel VF3 的 Stage 1 战斗；Opera Gex 冷/热启动与两种退出来源，以及 NES、Lutro、NeoCD 退出回归。

合并最新主干后，runtime 324 个测试文件 / 1874 项测试通过；Retrom backend-check、web-check、api-check 和独立 PostgreSQL integration-test 通过。协议代际门禁通过。MAME 14 项 Python 测试、完整核心构建和配套 DAT 校验通过；11980 条 DAT 事实语义不变。ONS PR 的 Web 构建、浏览器存档检查及其他平台构建通过。

## 验收边界

Daphne 尚缺完整 ROM、framefile 和视频。部分电脑或主机样例只验证到可交互菜单/剧情，未完成全部关卡内输入与逐核心存档恢复；没有将标题、空白画面、帧计数或 ready 计为完整游玩通过。ONS 的原问题《银色》已验证，早期《星之梦》样本不计作已修复。测试使用 Chrome + SwiftShader，不代表实际 GPU 性能。

缓存修复不改变存档格式；MAME 和 ONS 核心文件本身有变化，历史存档的核心风险提示继续生效。本轮不提升全部 T01–T47 或全核心产品验收状态。runtime #141 保留未完成的完整游玩验收追踪。

本地逐项清单与完整证据保留在工作区 `output/retrom-core-cache-20261013/` 和当前 PFB 的 `.pfb/evidence/`，不提交 ROM、凭据、截图或浏览器存储。
