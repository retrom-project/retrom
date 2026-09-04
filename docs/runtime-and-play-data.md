# Runtime Provider 与游玩数据

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已实施 / 一期权威基线 |
| 版本 | 2.1 |
| 日期 | 2026-09-04 |
| 协议事实源 | `api/runtime-provider/v1/`、`api/domains/runtime.yaml` |

## 1. 架构边界

Retrom Host 不实现、注册或选择具体运行引擎。运行时只有两个可部署 Provider：`emulatorjs` 与 `retrom-runtime`；前者声明 35 个 Target，后者声明 12 个 Target。Provider Bundle 内的 declaration 是 Target、能力、checkpoint contract、静态文件和客户端模块的唯一事实源。

后端启动时验证并激活 Bundle，把每个产品 Core 绑定到精确的 `providerId/providerVersion/providerApiVersion/bundleSha256/targetId/gameCompatibilityLine/targetContractSha256`。数据库、Go 服务和前端不得复制 Target 到引擎、入口文件或行为实现的映射。`runtime-target-bindings/v1/catalog.json` 只把产品 Core 绑定到 Provider Target；它不是第二份 Target registry。

## 2. Launch Envelope V1

`GET /runtime/launches/{launchId}/config` 只返回 `LaunchEnvelopeV1`。顶层固定为：

- `schemaVersion=1`
- `session`：Launch 身份、用途、模式和 Host 展示上下文；其中 `title/platformName/coreName`
  分别是游戏、平台与产品 Core 的冻结显示名，`coreName` 不参与 Provider Target 选择
- `runtime`：冻结的 Provider、Bundle、Target、模块、能力和 checkpoint contract
- `resources[]`：有序、带类型、大小、摘要与 Range 要求的授权内容
- `targetOptions`：Provider 私有、由该 Target 的内联 `targetOptionsSchema` 精确闭合的选项对象；没有判别字段
- `restore`：可空的不透明 checkpoint 输入
- `validation`：可空的运行验证输入
- `netplay`：可空的联机输入

Go 在返回前先执行 envelope 的共享结构校验，再用已激活 Target declaration 中的 `targetOptionsSchema` 精确校验选项；未知字段、缺少必填字段、错误类型和越界值都不能进入 envelope。前端 dispatcher 只负责 JSON-safe、深度、fan-out 与 16 KiB 大小边界，不复制任何 Target schema；Provider Module 在 mount 前用自己随 Bundle 发布的同一 schema 再做一次精确校验。模块 URL 越界、摘要或身份漂移、超出 JavaScript safe integer 的数字同样一律拒绝。

`targetOptionsSchema` 是受限、闭合的 JSON Schema 形状，只支持 object/array/string/integer/boolean、显式 nullable、安全相对路径、枚举和有界长度/数量；根必须是 `additionalProperties=false` 的 object。schema 本身进入 `targetContractSha256`，因此增加、删除或收紧选项都会形成新的 Target contract，不能在同一摘要下静默改变行为。

## 3. Provider dispatcher

Player 只有一个 dispatcher：

1. 校验 `runtime.moduleUrl` 位于 `/runtime/providers/{providerId}/{bundleSha256}/`；
2. 读取模块并验证 `moduleSha256`；
3. 动态导入 Provider Module V1；
4. 验证模块导出的 API 版本和 Provider 身份；
5. Provider Module 按自身 Target schema 精确校验 `targetOptions` 后调用 `mount(envelope)`，并取得标准 `PlayerRuntimeV1`；
6. 在退出、错误或 React 卸载时恰好一次执行清理。

Host 只消费标准能力和事件，不按 `providerId`、`targetId`、引擎或游戏类型分支。Provider 实现暂停、音量、输入过滤、视频模式、换盘、截图、帧计数、checkpoint、联机端口和 unique-origin 等行为；不支持的能力必须在 declaration 与运行对象上同时明确缺失。

## 4. 静态 Bundle 与内容资源

Provider 静态文件只从 `/runtime/providers/{providerId}/{bundleSha256}/{runtimePath}` 提供。路径必须命中已验证 Bundle 的 closed allowlist，服务端在安装时和读取时校验 size/SHA-256；未知 Provider、摘要、路径、MIME、查询或本机字节漂移均 fail closed。成功响应是 immutable 且带强 ETag。

游戏、BIOS、parent、多盘、项目文件、运行包和 cart 不属于 Provider Bundle，通过 envelope 的 `resources[]` 授权。公开资源 kind 固定为无版本后缀的语义标识：`ROM_BLOB`、`FILE_TREE`、`SEEKABLE_BLOB`、`NATIVE_WEB`、`ISOLATED_WEB`、`BIOS_BUNDLE`、`PARENT_ARCHIVE`、`MULTI_DISC`、`EXTERNAL_FILE_SET`、`WASM4_CART`。每个资源固定 role、ordinal、kind、URL、size、SHA-256 与 Range 要求。Provider 不能读取 envelope 未声明的内容，也不能从文件扩展、标题或 Core 名称猜测输入。

内容 kind、资源 kind、detector/delivery profile、launch/review policy 等长期语义 ID 都不携带 `_Vn`；其兼容变化由 schema/catalog/contract digest 表达。只有可被独立解析或散列的真实格式与域分隔符保留显式版本，例如 `Launch Envelope V1`、checkpoint format、`RETROM_FILESET_V1` 和 hash domain。来源 M3U 只是 `MULTI_DISC` 内容的 parser/证据表示，不成为另一种持久内容 kind。

内容 URL 由冻结输入生成稳定 identity；输入任一字节、依赖选择或影响输出的选项变化都产生新 URL。同一输入可在不同 Launch 中复用浏览器私有缓存，但服务端每次网络请求仍验证 grant 和 Launch 状态。

## 5. Checkpoint

Checkpoint 对 Host 是不透明字节。Target declaration 固定 `writeFormat/readFormats[]/maxBytes`。Provider 通过标准事件发布有序 availability，Host 只根据声明控制 UI。上传 metadata 只含 `checkpointFormat`、可选 name 和可选 discIndex；后端验证格式、大小与 SHA-256 后原样保存，不解析引擎私有内容。

恢复 Launch 只有在当前 Target 的 `readFormats` 包含存档格式，且内容、兼容线、依赖快照和 Target contract 满足规则时才返回 `restore`。升级只向前：新 Bundle 可以声明读取旧格式；降级、同版本换字节、删除仍被引用 Target 或把历史格式从可读集合移除都会阻断 readiness。不存在运行时回滚或旧 Bundle fallback。

## 6. 产品行为

EmulatorJS Provider 自己维护 35 个 Target 的 EJS core、配置、多盘和联机 profile。Retrom 不维护 EJS registry，也不向页面暴露引擎私有 globals。

`retrom-runtime` Provider 自己维护 12 个 Target，覆盖 EasyRPG、mkxp、MV/MZ、ONS、KiriKiri、Butterscotch、TyranoScript 与 WASM-4 等行为。项目索引、seekable blob、file tree、native web 和 cart 通过资源 kind 表达；引擎专属 checkpoint codec、输入、渲染、OPFS/Range 策略、unique-origin bootstrap 与清理均留在 Provider 内。Butterscotch 保持真实 `640×480` backing buffer，并由 Provider 根据运行 surface 实时计算等比最大内接的 CSS display 尺寸；横屏和竖屏都必须在不拉伸、不裁剪的前提下至少贴满一条边，不能退回固定 `640×480` 小窗。当前 TyranoScript 固定来源使用 r7 bridge；被项目脚本移出 DOM 的缓冲媒体由 bridge 中止遗留请求，避免快速跳过视频后耗尽同源连接。

RPG Runtime Validation 继续使用相同 Provider Module 与 envelope。`validation` 只携带服务端验证状态和 probe 输入；Provider 发布标准帧、输入、截图与 checkpoint 能力，RPG gate driver 在标准运行对象之上推进 14 个有序 gate，不创建另一套 Player。

## 7. 联机

联机资格由 Target 的 `netplayCompatibilityLine`、声明能力和 Retrom 的受控 profile 共同决定。`netplay` 字段只在 `session.mode=NETPLAY` 时存在，包含房间、会话、玩家序号、socket URL 和冻结 profile。

Host 通过 `PlayerRuntimeV1.netplayPort` 交换标准化联机消息；EJS 内部回调、控制数量和 rollback 设置属于 EmulatorJS Provider。单机 Launch 不得取得联机凭据，联机 Target 不匹配或不声明端口时创建 Launch 即失败。

## 8. unique-origin

Native Web Target 仍使用每 Launch 独立 origin。Provider 的 Target 选项只提供当次 bootstrap/entry/cleanup 所需输入；Host 以 sandbox iframe mount。服务端逐请求验证 exact Host、父 origin、一次性 ticket、HttpOnly capability、Launch 状态和封闭路由。

项目脚本不能取得应用 cookie、普通 API 或其他 Launch 内容。清理会撤销 capability、过期 cookie 并清空该 origin 存储；Provider 卸载与 Launch finish 都必须触发清理。

## 9. PlaySession 与游玩统计

Provider 报告真实 ready/start 后，Host 才调用 `POST /runtime/launches/{launchId}/start`。heartbeat 使用连续序号上报上一时段的 running/visible/paused；服务端以接收时间计费。finish 在已启动与未启动两种状态下都幂等撤销 Launch。

页面隐藏、暂停、断网、重复序号、跳号和 hard expiry 不能伪造时长。Provider 错误不会绕过 finish；卸载失败由服务端 expiry 最终收口。

## 10. 验证矩阵

实现变更至少通过：

- Provider Bundle schema、完整性、确定性、恶意归档与来源边界测试；
- 47 个 Target 与产品 Core binding 闭包测试；
- Go/TypeScript 对同一 Launch Envelope fixtures 的接受与拒绝测试；
- Go/TypeScript 对同一 Provider-owned `targetOptionsSchema` fixtures 的接受与拒绝测试，以及 schema 改变必然改变 Target contract digest 的测试；
- 静态门禁拒绝语义 ID 中的 `_Vn`，只允许显式列出的序列化/checkpoint/hash-domain 格式；
- dispatcher 的模块 URL/hash、身份、mount、能力与 cleanup 测试；
- checkpoint 跨 Launch 往返、格式/大小/摘要和降级拒绝测试；
- EmulatorJS 单机、多盘、沉浸输入与 8 个联机 profile 测试；
- `retrom-runtime` 12 个 Target 的 Product、Review Preview、Runtime Validation 与 unique-origin 测试；
- `ACC-PROVIDER-001` 至 `ACC-PROVIDER-008`，以及受影响的现有 Player/RPG/沉浸产品 Case。

PFB 必须从同一隔离 worktree 运行 Retrom/runtime 源码，但不生成 Provider candidate Bundle 或 Retrom 镜像。它以已验证的基座 Provider 提供 manifest、Target contract和大体积资产，只叠加按 revision 校验的 loose开发模块/本地adapter资源；该开发层不能进入production lock、release input或正式镜像。最终验收以 `make pfb-verify`、真实浏览器产品链和正式发布流程自身的确定性构建摘要为准。
