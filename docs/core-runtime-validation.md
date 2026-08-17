# 核心运行时验证基线

## 1. 文档职责

本文定义 EmulatorJS 核心的产品链路验证边界。核心版本、artifact、SHA-256、DAT 与 adapter 映射以 [`data/dat/`](../data/dat/) 的版本化 manifest 和前端 adapter registry 为机器事实源。公开 mGBA 产品 E2E 使用 [`testdata/public-roms/gba-smoke/`](../testdata/public-roms/gba-smoke/) 中由本项目源码确定性生成并许可分发的 ROM；需要操作者授权的 ROM、BIOS 与源归档统一放在 Git 忽略的 `data/game/`。两类资源都只由实际消费它们的产品测试维护路径和完整性约束。`make dev` 的 `.dev-data/` 服务器导入语料不属于测试 fixture。

独立 HTML 页面直接装载 EmulatorJS 会绕过 Retrom 的导入、审核、发布、Launch capability、内容端点和 Player，因此不能作为产品集成或验收证据。仓库不再维护这种 example runner，也不再用逐核心独立页面的成功结果宣称 Retrom 已覆盖对应核心。

## 2. 当前产品链路覆盖

| 核心/能力 | 实际入口 | 本机资源 | 覆盖边界 |
| --- | --- | --- | --- |
| `mgba` | `make web-e2e`、`ACC-RUN-002` | `testdata/public-roms/gba-smoke/gba-smoke.gba`；项目自有 MIT 夹具，size/SHA-256 与生成一致性由消费者锁定 | 真实 Retrom 服务、导入/发布数据、Launch、受限内容端点、Player 与 Chrome |
| `fceumm`、`fbneo` 联机 | `ACC-NP-001`–`009` | `data/game/netplay/`；大小与 SHA-256 由 `scripts/acceptance/seed-netplay.py` 锁定 | 真实 Retrom 服务、两个独立 Chrome process、房间/中继、Player frame/state hook |
| `fbalpha2012_cps1`、`fbalpha2012_cps2` | `go test -tags='integration localfixtures' ./internal/libraryimport -run '^TestFBA2012RealDATImportVariantAndLaunchIsolation$'` | `data/game/fbalpha2012_cps1/1941.zip`、`data/game/fbalpha2012_cps2/sgemf.zip` | 真实 DAT、导入、READY Variant、Launch 与跨家族 DAT 拒绝；不包含浏览器帧执行 |
| Saturn 多盘 | `ACC-MDISC-001`–`008` | 普通测试使用确定性临时夹具 | 产品 parser、导入、发布、Launch 内容协议、Player adapter 换盘与存档恢复；当前不包含真实 ROM 的浏览器运行 |

其余 enabled core 目前只有 manifest/schema、依赖物化、adapter 配置与相邻纯逻辑测试，尚没有走完整 Retrom 产品链路的真实浏览器 E2E。发布或依赖升级不能把这些结构检查解释为“核心已实际启动”。新增覆盖时应扩展 `make web-e2e` 或对应产品 E2E；项目自有且可再分发的确定性测试程序放在 `testdata/public-roms/`，其他内容只从 `data/game/` 读取本机授权资源。

## 3. 验证原则

- 浏览器运行证据必须从 Retrom 页面进入，并经过后端生成的 Launch config 与受限内容端点；不得直接把 ROM URL 传给独立 EmulatorJS 页面。
- 单元测试使用小型确定性字节、临时目录和临时 SQLite；公开产品 E2E 可以读取仓库自有且许可清晰的确定性测试 ROM，只有确实需要操作者私有 ROM/BIOS 的产品集成/E2E 才读取 `data/game/`。
- 私有资源测试必须使用 `localfixtures` build tag 或显式 E2E 前置检查，缺失时清楚报告本机输入缺失；默认单元测试和 CI 不得依赖专有内容。
- 资源的大小和 SHA-256 应由最接近的实际消费者校验，不能依赖一个与产品链路分离的全局 example manifest。
- 修改共享 loader、runtime config、内容字节协议、adapter 或存档协议时，运行所有现有受影响产品 E2E；对没有产品 E2E 的核心必须在交付说明中列为未覆盖，不能用独立页面或历史截图补齐。

## 4. MAME2003 版本覆盖

EmulatorJS 4.2.3 发布包中的官方 `mame2003` bundle `2.0.2` 曾在 Chrome 环境中于 `retro_load_game` 阶段稳定触发 WASM `unreachable`。当前依赖 manifest 因此为 `mame2003` 精确锁定 EmulatorJS 4.2.1 bundle `2.0.1`，同时保留 4.2.3 loader/UI。覆盖 URL、大小、SHA-256、build report、core source commit 和理由由 [`data/dat/emulatorjs/4.2.3/manifest.json`](../data/dat/emulatorjs/4.2.3/manifest.json) 维护。

实现必须满足：

- Core 保存实际 artifact 路径、SHA-256 和兼容配置，不能只保存 EmulatorJS 版本；
- resolver 对 `mame2003` 读取版本化覆盖，不能用 `mame2003_plus` 静默替代；
- 后续升级先通过实际 Retrom 产品 E2E，再删除覆盖；
- 覆盖变化视为 CoreArtifact 变化，旧存档不得默认跨版本加载。

## 5. 内容与运行时约束

GBA 单成员 ZIP 在导入时应逐字节物化实际 `.gba` entry 到 CAS；原始上传 Blob、archive entry 与派生内容保持可追溯。浏览器启动时不得临时猜测 ZIP 入口。

`dosbox_pure`、`genesis_plus_gx_wide` 与 `azahar` 使用 EmulatorJS `4.3.0-pre` 定向覆盖，其他核心使用各自 manifest 锁定版本。线程核心要求同源响应具备 COOP、COEP 与 CORP，生产 CSP 不得为运行时放开无界 `unsafe-eval`。

PSP profile 接受 raw `.cso` 和 `.iso`，两者分别以 `RAW_FILE_V1` 进入 CONTENT、Variant 与 Launch；产品导入、Launch 与 Player 不做格式互转。PPSSPP 与 `mednafen_psx_hw` 的线程 artifact basename、assets 和 renderer 配置以依赖 manifest 与 adapter 实现为准，不从旧的示例物化脚本推导。

## 6. 依赖升级

升级 EmulatorJS、任一 core、adapter registry 或 Arcade DAT 时执行 [`ACC-DAT-006`](./project-acceptance.md)，并运行 `make data-check`、`make deps-check`、受影响的产品集成测试和 `make web-e2e`。如果升级影响的核心尚无实际产品 E2E，验收结果必须明确记录该缺口；补足覆盖后才能据此声称浏览器运行兼容。
