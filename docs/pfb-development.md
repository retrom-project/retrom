# PFB 轻量开发容器

## 定位

PFB（Personal Feature Branch）是并行、多仓库联调环境，不是发布候选构建器。每个 PFB 把 Retrom 与 `retrom-runtime` 源码直接 bind mount 到一个开发容器：Go 由 `go run` 启动，Web 由 `next dev --webpack` 提供 HMR，runtime watcher 只重建浏览器 Provider 模块和仓库内的小型 adapter 资源。正式 Provider archive、core/EmulatorJS 产物、生产镜像与 release input 仍由原有独立发布流程生成和验证。

日常生命周期必须满足：

- `pfb-up` 只执行 Compose `up --no-build`，不运行 `npm ci`、Provider 打包、core 构建或数据复制；
- `pfb-restart` 只重启已有 app 容器；
- 文档、Go、Web 或 runtime adapter 源码变化不产生 source-stale/candidate-stale 状态，也不切换数据库；
- `pfb-build` 仅在首次使用，或 Dockerfile/Compose/entrypoint、Go/Node/package lock/API 生成输入变化后显式执行；镜像 tag 由工具链定义摘要决定，已有相同镜像不会重建；
- core 只通过 `pfb-core-build PFB=<name> CORE=<id>` 显式构建，绝不成为 init/build/up/restart 的隐式依赖。

## 目录与隔离

PFB 的稳定状态根固定为当前 Retrom worktree 的：

```text
.pfb/workspace/
├── data/                 # SQLite、CAS、上传、secret
├── providers/
│   ├── active.json       # 已完整验证的基座 Provider identity
│   ├── installed/        # 基座 manifest 与大体积静态资源
│   └── dev/              # current descriptor + 不可变 revisions
├── web-node/
├── runtime-node/
├── next/
├── go-cache/
├── home/
└── toolchain.json
```

两个 PFB 的上述路径、容器、Compose project、应用 Host、runtime Host、Cookie/Launch capability 均不同；共享的只有只读 Docker 工具链镜像、owner-local registry 和绑定 `127.0.0.1:3000` 的网关。一个 PFB 的 down/restart/reset/remove 不得扫描或修改另一个 PFB 的 workspace、容器或 registry entry。

PFB ID 从逻辑名称确定性派生，因此同一 spec 的稳定 URL 始终是 `http://<pfb-id>.localhost:3000`；Launch runtime 使用 `http://<launch-id>.rpg.<pfb-id>.localhost:3000`。down/up/restart、源码修改和兼容 migration 都不能改变 ID、URL 或数据根。

## Loose dev provider

PFB 启动前，`retrom-runtime/scripts/pfb-provider-watch.mjs --once` 从当前基座 active descriptor 与 integrity 文件读取 asset index/Target contract，使用 esbuild 只生成自包含 `client.mjs`，并复制 `provider-sources.json` 声明的本地 adapter 资源。文件摘要列表决定 `revision`；文件先在 staging 完成，再原子 rename 到 `dev/revisions/<revision>/` 的不可变目录，最后才原子切换 `dev-provider.json`。旧 Go 进程始终读取启动时绑定的旧 revision，新进程重启后才读取新 revision，因此 watcher 与 restart 之间也不会出现 module SHA/ETag/响应字节错配。常驻 watcher 监听 `src/`、`assets/`、package 与 provider source 声明。

Go 在启动时验证：descriptor 严格字段、provider/base bundle 身份、路径闭合、排序、size/SHA-256/media type、revision 和所有 override 都属于基座公开文件。开发文件沿原 `/runtime/providers/<provider>/<base-bundle>/...` 路径返回，使用 `Cache-Control: no-store` 与开发 ETag；Launch Envelope 保留基座 bundle/Target contract，但 `moduleSha256` 使用开发模块摘要。当前阶段 watcher 更新后执行一次 `pfb-restart`，让 Go 重新加载新 revision；无需 `pfb-build`。

`RETROM_PROVIDER_DEV_ROOT` 是失败关闭边界：只有 `RETROM_MODE=test`、非空合法 `RETROM_PFB_ID` 与匹配的本地 PFB origin 同时成立时才接受；普通 `make dev` 两者都不设置，release 模式对任何 loose root 无条件拒绝。生产 active descriptor、Provider archive、release digest、双镜像和 CI 不读取 `.pfb/`。

## 命令

首次初始化必须显式给出同一 PFB 树中的 runtime worktree；有 core 时再给出 `CORE_ROOTS`：

```bash
make pfb-init \
  PFB=feat-example \
  RUNTIME_ROOT=/absolute/path/.worktree/feat-example/project/retrom-runtime \
  CORE_ROOTS='{"butterscotch":"/absolute/path/.worktree/feat-example/project/retrom-core/Butterscotch"}'
make pfb-validate PFB=feat-example
make pfb-build PFB=feat-example
make pfb-provider-import \
  PFB=feat-example \
  SOURCE_ROOT=/absolute/path/to/verified-provider-base \
  CONFIRM=<actual-pfb-id>
make pfb-up PFB=feat-example PFB_SELECT=false
```

`SOURCE_ROOT` 的固定布局为 `active.json` 与 `installed/`。它可以指向另一个已验证开发环境的 Provider 基座；若本机尚无基座，可先在任一 Retrom checkout 中显式执行一次正式 Provider 准备，并把输出写到共享目录：

```bash
make runtime-provider-prepare \
  RETROM_PROVIDER_INSTALLED_ROOT=/absolute/shared/provider-base/installed \
  RETROM_PROVIDER_ACTIVE_PATH=/absolute/shared/provider-base/active.json
```

`pfb-provider-import` 只允许在 app 停止时执行。它先完整验证来源，复制到 staging 后再次验证，再执行 upgrade-only 检查；相同 bundle 幂等复用，不删除旧的 immutable installation，最后才原子切换 `active.json`。它不读取 Provider tar、不联网，也不构建 runtime/core。旧 PFB 已经拥有命名卷时使用下节的 `pfb-migrate-storage`，无需重复 import。

后续循环：

- Web：保存文件后等待 HMR，不执行 PFB 命令；
- Go：保存文件后执行 `make pfb-restart PFB=feat-example`；
- runtime adapter：等待 status 中 `providerDevRevision` 改变，再执行一次 `pfb-restart`；
- package lock、Go module、API 生成输入或工具链定义变化：停止 app，显式执行一次 `pfb-build`，再 up；
- core 源码：只有确实需要新 core 字节时显式执行 `pfb-core-build CORE=<id>`，然后按对应 runtime adapter 的消费方式联调。

`pfb-status` 是只读状态：报告容器/health、稳定 URL、workspace 和 provider revision，不计算整棵源码 digest，也不存在 `STALE`。`pfb-verify` 在运行实例上执行 PFB contract，并把 evidence 写到 `.pfb/evidence/`。

## 旧存储迁移与数据操作

旧版命名卷只允许在 app 停止时迁移：

```bash
make pfb-migrate-storage PFB=<name> CONFIRM=<actual-pfb-id>
```

迁移器只选择该 ID 前缀下、旧 state 指向的数据卷以及同 PFB 最新 Node/runtime-node/Next/Go cache；卷只读挂载。所有内容先复制到同一 `.pfb/` 文件系统的 staging，逐文件/符号链接内容指纹一致后原子 rename 为 `workspace`。`migration.json` 记录源卷，重复调用幂等；旧卷不会删除。

兼容 schema migration 始终原地升级同一 workspace。只有分支明确引入真正不兼容的数据语义时，先 down，再执行：

```bash
make pfb-data-reset PFB=<name> CONFIRM=<actual-pfb-id>
```

旧 `data/` 会移动到 `workspace/reset-backups/<UTC>/`，随后建立空数据根；provider、Node、Next、Go cache、ID 和 URL 保留。

`pfb-down` 只停止并保留全部状态。`pfb-remove ... CONFIRM=<id>` 移除 app 容器和 owner-local registry entry但保留 `.pfb/workspace`，可重新 init 注册。`pfb-destroy ... CONFIRM=<id>` 删除该 Retrom worktree 的整个 `.pfb/`；Git worktree/分支和迁移前旧命名卷仍不删除。根工作区的交互式 `make pfb-remove PFB=<name>` 另负责 clean preflight 后移除 Git worktree。

## 验证基线

实现或修改 PFB 时至少证明：

- init/build/up/restart 中没有 Provider tar、all-provider/core builder、每次 `npm ci` 或 Compose `--build`；
- runtime adapter 修改只改变 dev revision，轻量 restart 后实际 Launch 的模块 SHA/响应字节同步；
- Web HMR、Go restart、数据/URL 跨 down/up/restart 保持；
- release 模式拒绝 loose root，release/CI 命令不读取 `.pfb/`；
- 新 PFB 可从正式准备输出或另一已验证基座无歧义导入，重复导入幂等，降级与同版本重构建均被拒绝；
- core build 只由带精确 ID 的显式命令触发；
- 两个 PFB 的可写路径、Host、Cookie、Launch 与 provider dev revision 互不影响；
- legacy migration 幂等且保留源卷，reset 有可恢复归档，remove/destroy 都需要 exact ID；
- 真实游戏链完成登录、详情、Launch、Provider dispatcher、画面、输入和需要的 checkpoint/恢复。对于 Butterscotch，canvas 必须保持 640×480 backing，并成为 runtime surface 内最大居中的 4:3 矩形。
