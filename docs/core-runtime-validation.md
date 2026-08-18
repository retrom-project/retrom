# 核心运行时验证基线

## 1. 文档职责

本文定义 EmulatorJS 核心的产品链路验证边界。核心版本、artifact、SHA-256、DAT 与 adapter 映射以 [`data/dat/`](../data/dat/) 的版本化 manifest 和前端 adapter registry 为机器事实源。公开产品 E2E 使用 [`testdata/public-roms/`](../testdata/public-roms/) 中由本项目源码确定性生成并许可分发的 GBA 与 Arcade 测试程序。自动化测试不读取操作者私有 ROM/BIOS；`make dev` 的 `.dev-data/` 服务器导入语料也不属于测试 fixture。

独立 HTML 页面直接装载 EmulatorJS 会绕过 Retrom 的导入、审核、发布、Launch capability、内容端点和 Player，因此不能作为产品集成或验收证据。仓库不再维护这种 example runner，也不再用逐核心独立页面的成功结果宣称 Retrom 已覆盖对应核心。

## 2. 当前产品链路覆盖

| 核心/能力 | 实际入口 | 本机资源 | 覆盖边界 |
| --- | --- | --- | --- |
| `mgba` | `make web-e2e`、`ACC-RUN-002` | `testdata/public-roms/gba-smoke/gba-smoke.gba`；项目自有 MIT 夹具，size/SHA-256 与生成一致性由消费者锁定 | 真实 Retrom 服务、导入/发布数据、Launch、受限内容端点、Player 与 Chrome |
| `mame2003` | `make web-e2e`、`ACC-RUN-006` | `testdata/public-roms/arcade-smoke/`；项目自有 MIT 的 Z80 程序、图形/声音资源、小型 MAME XML 和测试 BIOS 角色归档 | `ACC-DAT-004` 独立证明 release 真实 DAT 的物化/选版；产品 E2E 将项目自有小型 DAT 通过测试装置登记为 test-only `BUILTIN`（无上传/API），再覆盖 Split Child/Parent/BIOS 识别、审核与发布 schema v2、详情页同一 DatVersion、首次启动重验证仍保持 v2、schema v2 current revision 直接 Launch、三路受限内容、Player、Chrome 动画帧与运行遥测；测试 BIOS 不被 Pac-Man 驱动执行，不证明核心内部 BIOS 执行语义 |
| `fbneo` | `make web-e2e`、`ACC-RUN-007` | `testdata/public-roms/arcade-smoke/fbneo/`；项目自有 MIT 的 Z80 程序、生成图形/PROM、Logiqx DAT 和测试 BIOS；生成器将其控制的 4 bytes 校正到锁定驱动所需 CRC32，完整 bytes 另由 SHA-1/SHA-256 固定 | `ACC-DAT-004` 独立证明 release 真实 DAT 的物化/选版；产品 E2E 将项目自有小型 DAT 通过测试装置登记为 test-only `BUILTIN`（无上传/API），再覆盖 Split Child/Parent/BIOS 识别、审核与发布 schema v2、详情页同一 DatVersion、首次启动重验证仍保持 v2、schema v2 current revision 直接 Launch、三路受限内容、Player、Chrome 动画帧与运行遥测；测试 BIOS 不被 Pac-Man 驱动执行，且只证明单机路径，不证明双浏览器联机 |
| Saturn 多盘 | `ACC-MDISC-001`–`008` | 普通测试使用确定性临时夹具 | 产品 parser、导入、发布、Launch 内容协议、Player adapter 换盘与存档恢复；当前不包含真实 ROM 的浏览器运行 |

其余 enabled core（包括 FCEUmm 与 FBA2012）目前只有 manifest/schema、依赖物化、adapter 配置、协议或相邻纯逻辑测试，尚没有走完整 Retrom 产品链路的真实浏览器 E2E。发布或依赖升级不能把这些结构检查解释为“核心已实际启动”。新增覆盖时应扩展 `make web-e2e` 或对应产品 E2E，并使用项目自有或有明确再分发许可、可确定性生成且能够提交的测试程序。FBNeo 虽已有单机产品 E2E，但仍没有双浏览器联机运行基线。

## 3. 验证原则

- 浏览器运行证据必须从 Retrom 页面进入，并经过后端生成的 Launch config 与受限内容端点；不得直接把 ROM URL 传给独立 EmulatorJS 页面。
- 单元测试使用小型确定性字节、临时目录和临时 SQLite；产品 E2E 只读取仓库自有且许可清晰、生成源可审查的确定性测试 ROM。
- 自动化测试不得依赖操作者私有 ROM/BIOS，也不得在测试期间联网下载第三方游戏内容；没有合法公开 fixture 的核心必须明确登记为未覆盖。
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
