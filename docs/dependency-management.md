# Provider、DAT 与运行依赖管理

| 属性 | 内容 |
| --- | --- |
| 文档状态 | 已实施 / 一期权威基线 |
| 版本 | 2.0 |
| 日期 | 2026-09-03 |

## 1. 依赖分层

运行依赖分为四类，不能互相代替：

1. Provider Bundle：可执行的客户端模块、Target declaration、静态文件、许可和 provenance；
2. Target binding catalog：Retrom 产品 Core 到 Provider Target 的精确绑定；
3. DAT/BIOS catalog：游戏识别、Arcade parent/machine 和静态 BIOS 要求；
4. 用户安装的 runtime asset pack：RTP 等由管理员提供、按内容冻结的运行输入。

Provider Bundle 是 Target 行为的唯一事实源。Retrom 的 binding catalog 只引用 Target 身份和 contract digest；DAT 只引用 `providerId/targetId/targetContractSha256`，不得重新声明入口、能力或引擎映射。

## 2. Provider Bundle V1

正式和 candidate 产物使用同一个闭合 Bundle V1 schema。Bundle 至少固定：

- Provider 身份与 API 版本；
- archive digest、逐文件 path/size/SHA-256/MIME；
- 客户端 module path 与 SHA-256；
- 全部 Target declaration；
- checkpoint、兼容线、能力与 Provider 私有选项 schema；
- source commit、上游 Release 坐标、许可证和 notice。

归档必须确定性生成：规范路径、顺序、mode、mtime、owner、压缩参数和 JSON 编码完全固定。同一输入连续构建两次必须得到相同 archive digest。绝对路径、`..`、链接逃逸、重复路径、大小溢出、未声明文件、摘要漂移或不完整许可都会使构建/安装失败。

## 3. 两个 Provider

`emulatorjs` Bundle 从 Retrom 仓库中锁定的 EJS upstream 输入生成，声明 35 个 Target。它独占 EJS core、core options、启动动作、多盘和 8 个联机 profile 的行为映射。

`retrom-runtime` Bundle 从独立仓库生成，声明 12 个 Target。`provider-sources.json` 只记录上游或本地 core 构建来源，不声明 Retrom 路由或产品 binding；Target registry 只存在于 Provider declaration。正式 package 校验 7 个上游输入及其锁定 source/tag/asset，源码构建或缓存复用都必须得到声明的字节。

## 4. Retrom binding catalog

`data/runtime-target-bindings/v1/catalog.json`（以仓库实际路径为准）为每个产品 Core 指定：

- `coreId`
- `providerId`
- `targetId`
- `targetContractSha256`

启动时必须证明 binding 全量闭合：Core 存在、Target 存在、Provider/API 版本正确、contract digest 完全相同、launch policy 可用。缺项、多项、未知 Target、重复绑定或摘要漂移都会阻断 readiness。前端不读取该 catalog；它只消费后端生成的 Launch Envelope。

## 5. 激活与只前进升级

Provider 安装和数据库 reconcile 在对外 ready 之前完成。激活事务登记 Bundle、Target 与 contract，验证所有当前 binding 和历史引用，然后一次切换 active identity。

系统只支持向前升级：

- `providerVersion` 必须高于已登记版本；
- 同版本不同 `bundleSha256` 永远拒绝；
- 新 Bundle 必须保留仍被产品、Validation、Launch 或 Save 引用的 Target；
- `gameCompatibilityLine` 不能对同一兼容产品静默改变；
- 新 Target 的 `readFormats` 必须覆盖仍要求恢复的历史 checkpoint 格式；
- 数据库见过更高版本时，较低版本即使字节完整也拒绝启动。

不提供降级、回滚或旧 Bundle fallback。升级失败保持服务未 ready，由部署者修复新输入并继续向前。

## 6. Candidate 与 production

PFB candidate 只由命名 worktree 中的 Retrom、`retrom-runtime` 和 core worktree 构建。PFB spec 固定各仓库 commit、dirty/source tree digest 和必要的本地 core source；构建前后都校验 stale，不能混入 baseline checkout 或另一个 PFB 的文件。

candidate descriptor 可以覆盖 `provider-sources.json` 已声明的上游来源，但不能注入未声明 Target、改写 Retrom binding 或伪造正式 Release 坐标。candidate Bundle 存放在 PFB 专用目录，由 PFB lock V2 引用。

production lock 只接受已授权的正式 Provider archive、descriptor 和 SHA-256。正式 `provider:build/provider:check/release:build` 发现 candidate 输入时必须失败；PFB 构建也不得误用 production lock。两种锁都引用同一 Bundle schema和验证器，不存在功能不同的开发 manifest。

## 7. 镜像与 release input digest

Retrom 镜像构建输入必须包含：

- Retrom source tree；
- 两个 Provider 的精确 descriptor 与 archive；
- Target binding catalog；
- DAT/BIOS manifests；
- OpenAPI 与 Launch Envelope schema；
- Web build dependencies。

`release-input-digest` 对上述输入做规范摘要。Docker build、后端启动日志、PFB lock 和验收证据必须报告同一 Provider identity。镜像内只复制已验证的 Provider stage，不在 build 时从网络解析 `latest`，也不允许运行容器从宿主源码目录补文件。

正式 release 可以因为尚未授权发布资产而暂不可执行；这不允许用 candidate 冒充 production。PFB 验收使用 candidate，正式发布授权后再生成 production lock 并重跑完全相同的验证链。

## 8. DAT 与 BIOS

EmulatorJS DAT 的 binding 使用 `(providerId,targetId,targetContractSha256)`。`data-check` 与启动校验都要求：

- Target 在已激活 EmulatorJS Bundle 中存在；
- contract digest 完全一致；
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

Provider archive 必须连续构建两次并比较 digest。PFB 另执行 `pfb-validate/build/up/verify` 和 `ACC-PROVIDER-001..008`。任何 schema、Target 数量、binding、digest、许可、来源、candidate/production 隔离或只前进规则失败都属于阻断错误。

## 11. 追溯与日志

诊断可以显示 Provider、Bundle、Target、contract digest、source commit、Release 坐标和验证结果，但不能暴露宿主路径、capability、私有游戏内容或上传 Blob 标识。许可证与 notice 随 Bundle 和镜像分发；应用 HTTP API 不提供任意宿主文件读取。

一次 Launch、Validation、Save 与联机房间都必须能追溯到精确 Provider identity。只记录版本或 Target 名不足以证明实际字节，必须同时记录 `bundleSha256` 与 `targetContractSha256`。
