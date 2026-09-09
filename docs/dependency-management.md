# Provider、DAT 与运行依赖管理

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已实施 / 一期权威基线 |
| 版本 | 2.2 |
| 日期 | 2026-09-08 |

## 1. 依赖分层

运行依赖分为四类，不能互相代替：

1. Provider Bundle：可执行的客户端模块、Target declaration、静态文件、许可和 provenance；
2. Target binding catalog：Retrom 产品 Core 到 Provider Target 的精确绑定；
3. DAT/BIOS catalog：游戏识别、Arcade parent/machine 和静态 BIOS 要求；
4. 用户安装的 runtime asset pack：RTP 等由管理员提供、按内容冻结的运行输入。

Provider Bundle 是 Target 行为的唯一事实源。Retrom 的 binding catalog 与 DAT 只引用稳定 `providerId/targetId` 和 Host 产品策略，不得重新声明入口、能力或引擎映射。Bundle 摘要只由需要重现实例字节的 Launch、Preview 与 Netplay session 冻结。

## 2. Provider Bundle V1

正式和 candidate 产物使用同一个闭合 Bundle V1 schema。Bundle 至少固定：

- Provider 身份与 API 版本；
- archive digest、逐文件 path/size/SHA-256/MIME；
- 客户端 module path 与 SHA-256；
- 全部 Target declaration；
- checkpoint、能力与 Provider 私有选项 schema；
- source commit、上游 Release 坐标、许可证和 notice。

归档必须确定性生成：规范路径、顺序、mode、mtime、owner、压缩参数和 JSON 编码完全固定。同一输入连续构建两次必须得到相同 archive digest。绝对路径、`..`、链接逃逸、重复路径、大小溢出、未声明文件、摘要漂移或不完整许可都会使构建/安装失败。

每个 Target 必须内联一个闭合 `targetOptionsSchema`。Retrom 权威 schema 只定义这套受限方言，Provider declaration 定义具体属性；Host 不维护 `optionsKind`、Target→选项映射或默认值。Go 在签发 envelope 前精确验证，Provider Module 在 mount 前以同一声明复核，Web dispatcher 只做通用 JSON 安全和资源上限。

Provider manifest 的 `providerApiVersion` 在结构层只要求正整数，使未来文档仍能被安全识别和持久化；当前 active loader 与安装器只接受 Provider API 1。结构可解析不等于实现可执行，未知 API 必须在激活前失败关闭。

## 3. 两个 Provider

`emulatorjs` Bundle 从独立 retrom-runtime 仓库中锁定的 EJS upstream 与 fork Release 输入生成，声明 44 个 Target。它独占 EJS core、core options、启动动作、多盘和 8 个联机 profile 的行为映射。

`retrom-runtime` Bundle 从独立仓库生成，声明包含 ScummVM、TIC-80、FAKE-08 和 Play! 在内的 17 个 Target；生产可用集合以正式 lock 为准。`provider-sources.json` 只记录上游或本地 core 构建来源，不声明 Retrom 路由或产品 binding；Target registry 只存在于 Provider declaration。正式 package 校验声明的上游输入及其锁定 source/tag/asset，源码构建或缓存复用都必须得到声明的字节。

## 4. Retrom binding catalog

`data/runtime-target-bindings/v1/catalog.json`（以仓库实际路径为准）为每个产品 Core 指定：

- `coreId`
- `providerId`
- `targetId`

启动时必须证明 binding 全量闭合：Core 存在、Target 存在、Provider/API 版本正确且 launch policy 可用。缺项、多项、未知 Target 或重复绑定都会阻断 readiness。前端不读取该 catalog；它只消费后端生成的 Launch Envelope。

catalog 中的 content kind、detector/delivery profile、launch/review policy 以及 Bundle 中的 resource kind 都是稳定语义 ID，不带 `_Vn`。序列化文档、checkpoint 编码和 hash-domain 标记可以保留显式版本；两类标识由静态门禁的窄 allowlist 区分，不能用格式版本名替代业务语义。

## 5. 激活与只前进升级

Provider 安装和数据库 reconcile 在对外 ready 之前完成。激活事务登记 Bundle 与 Target、验证所有当前 binding 和 checkpoint 格式引用，然后一次切换 active identity。

系统只支持向前升级：

- `providerVersion` 必须高于已登记版本；
- 同版本不同 `bundleSha256` 永远拒绝；
- 新 Bundle 必须保留仍被产品 binding 使用的 Target；
- 新 Target 的 `readFormats` 必须覆盖仍要求恢复的历史 checkpoint 格式；
- 数据库见过更高版本时，较低版本即使字节完整也拒绝启动。

不提供降级、回滚或旧 Bundle fallback。升级失败保持服务未 ready，由部署者修复新输入并继续向前。

## 6. PFB 开发层与 production

PFB 只消费同一命名 worktree 中的 Retrom 与 `retrom-runtime` 源码，不再构建或锁定完整 candidate Bundle。workspace 中已安装的基座 Provider 必须先通过正式 Bundle schema、integrity、Target declaration 与静态文件验证；runtime watcher 从基座读取 asset index，只重建 `client.mjs` 和 `provider-sources.json` 已声明的本地 adapter 资源。

loose descriptor 只能覆盖同一 provider/base bundle 中已有的公开路径，不能注入 Target、改写 Retrom binding、伪造 Release 坐标或替换未知大体积 core。Go 启动逐文件验证 size/SHA-256/media type与内含字节，并只在合法test PFB中接受；release 和普通非 PFB 进程拒绝 `RETROM_PROVIDER_DEV_ROOT`。

production lock 仍只接受已授权的正式 Provider archive、descriptor 和 SHA-256。正式 `provider:build/provider:check/release:build`、release input digest与双镜像不读取 `.pfb/`，也不能消费loose descriptor。PFB产品验证与正式归档/许可/确定性构建是两条互补门禁，PFB PASS不构成发布授权。

### ScummVM 核心资产

ScummVM fork 为 `retrom-project/scummvm`，当前构建基线是 `v2026.3.0` /
`fed42f2068dcafc6aafa1c28c77e4c88def74b66`。核心包同时提供共享 Wasm、105 个稳定一级引擎动态插件、
支持数据、许可和 Linux x86-64 原生检测器。浏览器冷启动需要共享 JavaScript/Wasm 主模块，随后只请求选定的引擎插件；其余插件留在 Provider 静态资产中。支持数据随引擎实际打开文件按需读取，不能把插件拆分理解为没有公共启动成本。
检测器从已验证 Provider 的 integrity 清单定位，启动时校验并复制到数据目录的私有摘要缓存，权限为 0500。
不会在启动时下载或构建工具；其他服务端架构在匹配工具交付前必须明确拒绝。

fork 的维护分支为 `retrom/2026.3.0`，`master` 仅镜像上游。正式核心 Release 从维护分支的不可移动
annotated tag 生成，同时发布 `scummvm-runtime.zip` 和 `rpg-runtime-release.json`。runtime 固定
repository、tag、commit、ABI、上游基线与归档准确大小/SHA-256，核对发布元数据后再解包；内部文件必须
与闭合 layout、引擎映射和逐文件摘要一致。检测器与 Web 核心必须来自同一归档。

PFB 的显式 core 覆盖仍只接受 fork 生成的 candidate 和逐文件摘要；尚未发布的来源通过
`developmentInputs` 登记。普通构建、正式 release 与 production lock 不接受未发布输入，正式 release
也拒绝任何本地 core 覆盖。发布依次完成 fork、runtime 固定输入和 Provider、Host 正式 lock，并对正式
归档复跑受影响的产品 Case。

## 7. 镜像与 release input digest

Retrom 镜像构建输入必须包含：

- Retrom source tree；
- 两个 Provider 的精确 descriptor 与 archive；
- Target binding catalog；
- DAT/BIOS manifests；
- OpenAPI 与 Launch Envelope schema；
- Web build dependencies。

`release-input-digest` 对上述输入做规范摘要。Docker build和后端启动日志必须报告同一生产Provider identity；PFB evidence另报告基座identity与开发模块摘要，不能冒充release digest。镜像内只复制已验证的Provider stage，不在build时从网络解析`latest`，也不允许运行容器从宿主源码目录补文件。

正式 release 可以因为尚未授权发布资产而暂不可执行；这不允许用PFB loose开发层冒充production。正式发布授权后生成production lock，并重跑归档完整性、确定性和受影响的相同产品链。

## 8. DAT 与 BIOS

EmulatorJS DAT 的 binding 使用稳定 `(providerId,targetId)`。`data-check` 与启动校验都要求：

- Target 在已激活 EmulatorJS Bundle 中存在；
- DAT 文件 size/hash、parser version 和 machine 数据闭合；
- 平台/Core/Target 映射唯一；
- 内置 DAT 更新不会删除仍被锁定 Variant 使用的事实。

BIOS Requirement 同样从 Target binding 和 DAT 生成，不从前端或 Provider 私有 registry 推断。安装内容按逻辑名、大小与摘要校验；游戏 Launch 冻结实际 installation/dependency snapshot。

## 9. Runtime asset pack

RPG RTP 等 pack 由管理员上传，经过安全归档扫描、路径规范化、文件数/总大小上限和逐文件摘要后安装。Pack definition 与 installation 分离；Target 只声明所需 slot/type，Retrom 冻结具体 installation。

有 Variant、Validation、Launch 或 Save 引用时不能删除 installation。替换 pack 产生新 dependency snapshot，不原地修改已经签发的 Launch 或历史 Save。

## 10. 开发与 CI 门禁

本地与 CI 至少执行：

```text
retrom-runtime:
  npm run lint
  npm run typecheck
  npm test
  npm run build
  npm run package:check
  npm run provider:input:check
  npm run provider:build
  npm run provider:check
  npm run release:build

Retrom:
  make data-check
  make prepare-deps deps-check
  make api-generate api-check
  make build test lint-go integration-test
  make web-lint web-typecheck web-test web-build
```

Provider archive 必须连续构建两次并比较digest。PFB另执行`pfb-validate/build/up/verify`、loose boundary与`ACC-PROVIDER-001..008`。任何schema、Target数量、binding、digest、许可、来源、PFB/production隔离或只前进规则失败都属于阻断错误。

### Ruffle 开发候选

Ruffle fork 为 `retrom-project/ruffle`，上游基线固定为
`e46d1642fb67a53b56ffa4b1871cb7c57589e36d`，维护分支为 `retrom/ge46d1642fb67`，`master` 保留上游镜像。
源码边和维护分支由当前 Retrom `workspace/manifest.yaml` 管理，不进入 workspace 根引导清单。
fork 显式构建 `ruffle.js`、`core.ruffle.js`、`ruffle.wasm` 和原始许可，输出有界 candidate 文件清单和逐文件 SHA-256。
runtime 的 `developmentInputs` 只聚合该候选，不编译核心、不虚构 Release tag；普通/正式构建拒绝未发布来源。
PFB 将完整、已验证 Provider 候选作为不可变基座导入，后续 adapter 修改走 loose watcher；核心字节变化必须显式重建
并通过更高的 Provider 候选版本重新导入。production lock 保持不变，正式发布仍需授权、固定 fork Release 和完整门禁。

## 11. 追溯与日志

诊断可以显示 Provider、Bundle、Target、source commit、Release 坐标和验证结果，但不能暴露宿主路径、capability、私有游戏内容或上传 Blob 标识。许可证与 notice 随 Bundle 和镜像分发；应用 HTTP API 不提供任意宿主文件读取。

Launch、Preview 与 Netplay session 必须用 `bundleSha256` 追溯到精确 Provider 字节；Validation 与 Variant 使用稳定 Provider/Target 和各自真实输入证据；Save 只保存 checkpoint format，并由恢复时的当前 Target `readFormats` 判定兼容性。

### Flycast 核心与开发候选

Flycast 的 workspace 依赖由 Retrom catalog 指向 `retrom-project/flycast-wasm` 的
`retrom/1.0` 维护分支。正式输入固定 `retrom-core-1.0-r1` Release，其 commit、
资产摘要、大小和 ABI 由 runtime 的 `src/providers/emulatorjs/source-catalog.ts` 声明。
PFB 中显式 `pfb-core-build CORE=flycast`，再由 runtime
`candidate:build` 消费同一 PFB 的闭合 candidate descriptor。Provider 校验仓库、ABI、
源码身份、成员集合和每个文件的大小/SHA-256，并将核心、report 和许可合入 EmulatorJS
4.2.3 的独立 Bundle。普通 release 构建拒绝未发布的 development input；候选构建不能
被解释为已发布核心版本。升级 core 字节须重建完整候选 Provider 并提高 Provider 版本，
不能用 loose watcher 覆盖已安装 Bundle 内的核心。

构建固定 nasomers v1.0 补丁、Flycast/RetroArch commit 和 Emscripten 镜像 digest。
源码桥接替代预编译 stub，链接拒绝未解析符号并核验 WASM JIT 所需 HEAP 导出；
核心归档与 Bundle 均携带对应许可。具体源码锁定值以 fork 的构建脚本及 runtime 的
`src/providers/emulatorjs/source-catalog.ts` 为准，BIOS 和游戏不进入核心或 Provider 归档。

### Play! PS2 核心

Play! 的核心源码与构建归属为 `retrom-project/Play-`，维护基线为上游 `83700b2c31e593bc94e845b4b31b797be84dda59`，维护分支 `retrom/g83700b2c31e5`。Retrom workspace catalog 将其登记为 runtime 的 `play` 核心依赖。固定 Emscripten 工具链、ABI `play-host-v1`、闭合资产与许可由 fork 管理，annotated `retrom-core-g83700b2c31e5-rN` tag 的工作流发布正式资产。

runtime 通过普通 `upstreamReleases` 固定 Play! tag、commit、metadata 与资产；Retrom 通过正常 Provider Release 锁文件消费它。PS2 核心保持启用，手动创建目录、导入与启动遵循普通核心流程，无实验开关或专用禁用状态；因运行不稳定，不提供推荐目录，具体行为见[游戏目录契约](./platform-instance.md#release-推荐目录-catalog)。候选与本机路径不进入 production lock；后续更新沿用 core → Provider → Host 的正常发布顺序。产品验证见 [ACC-PS2-001](./project-acceptance.md#acc-ps2-001play-ps2-按需光盘与即时状态)。
