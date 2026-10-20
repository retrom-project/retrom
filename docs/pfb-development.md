# PFB 开发

PFB 将一次开发的 Retrom、runtime 及必要核心源码放在同一命名工作树，并保留独立数据库、受管文件、Provider 和缓存。联合开发从 [retrom-project 工作区](https://github.com/retrom-project/retrom-project)开始。

## 前提与命令目录

使用 Linux x86_64 本地文件系统、Git、Make、Python 3、Docker Engine 及 Compose 插件；Docker 使用 `default` context，当前普通用户可访问 Docker。WSL 使用 Linux 文件系统，不将工作树放在 `/mnt/c`。按工作区说明准备 GitHub SSH 访问，不使用 root 或 sudo 运行开发命令。

下文从 `retrom-project/` 根目录开始。`make init`、`make validate`、`make check`、`make pfb-list` 和完整清理 `make pfb-remove` 属于工作区；`make -C "$TASK_RETROM_ROOT" ...` 明确调用当前 PFB 的 Retrom Makefile。Retrom 仓库没有 `pfb-list` 目标。

## 1. 准备源码与初始化

在已克隆的工作区根执行，选择一个未使用的 PFB 名称：

```bash
PFB=feature-example
TASK_WORKSPACE_ROOT="$(pwd -P)"
make pfb-list
make init PFB="$PFB" REPOS="retrom-runtime"
make validate PFB="$PFB"
make check PFB="$PFB" REPOS="retrom-runtime"

TASK_RETROM_ROOT="$TASK_WORKSPACE_ROOT/.worktree/$PFB/project/retrom"
TASK_RUNTIME_ROOT="$TASK_WORKSPACE_ROOT/.worktree/$PFB/project/retrom-runtime"
make -C "$TASK_RETROM_ROOT" pfb-init PFB="$PFB" RUNTIME_ROOT="$TASK_RUNTIME_ROOT"
make -C "$TASK_RETROM_ROOT" pfb-status PFB="$PFB" FORMAT=json
```

`init` 只准备源码，Retrom 始终包含在内；`REPOS` 精确选择且不展开依赖。新分支从清单 `defaultBranch` 刚 fetch 的远端提交创建，Retrom／runtime 使用 `codex/` 前缀，已有工作树不切换或重置。依赖由本 PFB 的 `retrom/workspace/manifest.yaml` 决定；不要用基线全局 `make update` 准备单个 PFB。

记录 `pfb-init`／`pfb-status` 返回的实际 `id` 和 `url`。只改文档时完成源码准备、初始化及相应静态检查即可；以下步骤用于需要实际运行产品的环境。

## 2. 准备 Provider 基座、工具链与浏览器

新 workspace 需要一份包含 `active.json` 和 `installed/` 的完整已验证 Provider 根。可用当前 Retrom pin 准备：

```bash
make -C "$TASK_RETROM_ROOT" runtime-provider-prepare
```

命令输出 `RETROM_PROVIDER_INPUT=<绝对目录>`，下一步将这个值作为 `SOURCE_ROOT`。也可使用已有的完整已验证基座；导入只读取和校验资产，不执行来源 PFB 的源码。共享下载缓存在工作区 `.cache/runtime-providers/`，以内容寻址分块保存所有已下载的 Provider 数据：新的 runtime pin 只下载变化的分块，导入时 Provider 文件以硬链接共享这些已验证的不可变字节，新 PFB 几乎不额外占用磁盘。本 PFB 的安装和 active 独立。

浏览器验证需要 Chrome for Testing 及配套 Node／Playwright 工具。缺少时从当前 Retrom 准备；已有匹配工具可通过 `RETROM_CHROME_EXECUTABLE` 指向其入口：

```bash
make -C "$TASK_RETROM_ROOT" prepare-e2e-browser
make -C "$TASK_RETROM_ROOT" pfb-validate PFB="$PFB"
make -C "$TASK_RETROM_ROOT" pfb-build PFB="$PFB"
```

`pfb-build` 准备开发镜像、依赖、生成代码，并发布不可变 `runtime-tools/<sha>` 工具快照；不构建核心或 Provider 分块。再次显式 build 复用未变化的镜像和依赖，仍会构建／发布所选 runtime 工具。当前 app 运行时拒绝 build，需要先停止当前 PFB。

将下面两个占位值分别替换为初始化返回的实际 ID，以及准备命令输出的 Provider 目录：

```bash
TASK_PFB_ID='replace-with-actual-pfb-id'
TASK_PROVIDER_BASE='/absolute/path/from/RETROM_PROVIDER_INPUT'
make -C "$TASK_RETROM_ROOT" pfb-provider-import \
  PFB="$PFB" SOURCE_ROOT="$TASK_PROVIDER_BASE" CONFIRM="$TASK_PFB_ID"
```

导入核验大小、摘要、integrity、proof 及完整基座后原子发布。候选同版本重建只在显式 PFB candidate 导入中允许；正式升级校验不因此放宽。已有旧命名卷的 PFB 应先走下文迁移，不把新建流程用于覆盖旧数据。

## 3. 启动、状态与素材

```bash
make -C "$TASK_RETROM_ROOT" pfb-up PFB="$PFB" PFB_SELECT=false
make -C "$TASK_RETROM_ROOT" pfb-status PFB="$PFB" FORMAT=json
make -C "$TASK_RETROM_ROOT" pfb-verify PFB="$PFB"
```

使用输出的 `http://<actual-pfb-id>.localhost:3000`。`PFB_SELECT=false` 保留裸 `localhost:3000` 当前选择；需要切换裸地址时，显式执行 `make -C "$TASK_RETROM_ROOT" pfb-use PFB="$PFB"`。标准 `make dev` 使用 `localhost:4000`。

每个环境的 `.pfb/workspace/` 持久保存 `data/`、`postgres/`、`providers/`、依赖和缓存。标准 app 运行源码 Go、Next dev 及两个已安装 Provider 的 watcher。共享 registry、锁和网关配置归工作区根 `.pfb/`；不能从这些目录或 registry 缓存推断运行状态。

来源素材通过只读 `/server-data` 挂载提供。默认来源是工作区 `.dev-data/`；可在当前 Retrom 的 `.pfb/server-data.json` 写入以下显式配置，将 `root` 替换为主机上已存在的绝对目录，再重新 down／up：

```json
{"root":"/absolute/path/to/your/sources"}
```

管理员在页面从 `/` 浏览容器实际可见目录，使用 `/server-data/...` 路径扫描。元数据与文件组织见[来源示例](import-source-examples.md)。游戏和 BIOS 接收后复制到本 PFB 的受管目录，来源仍只读。

## 4. 日常修改

| 修改 | 加载方式 |
| --- | --- |
| Web 页面与样式 | 等待 Next HMR |
| Go 代码 | `make -C "$TASK_RETROM_ROOT" pfb-restart PFB="$PFB"` |
| runtime 浏览器 adapter | 等待 status 的 `providerDevModuleSha256` 更新，再重启 app 加载新模块 |
| CLI／catalog 等不可变工具输入 | 停止当前 PFB，显式 build 发布新工具，再 up |
| Dockerfile／Compose／entrypoint、依赖锁或 API 生成输入 | 当前 PFB 执行 down → build → up |
| 核心二进制 | 显式构建核心及配套候选，再导入验证过的基座 |

日常 `up`／`restart` 不自动构建镜像、核心或 Provider 分块。不要手改 toolchain marker 绕过校验，也不要通过删除 workspace 强制重建依赖。

联合验收可在显式 `pfb-build` 时传 `RETROM_RUNTIME_TOOL_INPUT=<本 PFB 的自足工具目录>`，将完整 production 依赖复制到不可变快照；输入不得越出同一命名 PFB。工具清理后运行进程仍固定已发布的 resolved 路径。

核心变更先在本 PFB 准备 core worktree，并在停止态使用相同的 `pfb-init` 参数追加 `CORE_ROOTS` JSON 映射，例如 `CORE_ROOTS='{"<core-id>":"<同一 PFB 内核心 worktree 的绝对路径>"}'`，替换占位值后使用；存在核心时必须保留同树 `RUNTIME_ROOT`。`pfb-core-build PFB=<name> CORE=<id>` 只构建／验证该核心，输出 `.pfb/workspace/core-builds/<id>/current`，不会自动切换运行资源。按[核心接入流程](platform-cores-integration.md)装配同源候选和导入基座后，应核对实际 Run 的核心资产与 Target 指纹再记录验收。

watcher 为已安装的 `retrom-runtime`、`emulatorjs` 分别生成开发描述；`dev/provider-id` 决定使用显式 core-inputs 的 Provider，默认 `retrom-runtime`，其他 Provider 使用已验证基座。描述在同级 `files/` 写入摘要命名资源后原子发布，失败保留前一份有效结果；Host 验证文件后流式提供 Range／HEAD。开发 URL 绑定模块摘要并返回 `private, no-store, no-transform`，正式 Bundle URL 仍读取原基座。资源完整性及实际 Target 指纹分别核对，不能按 client 整包 SHA 推断存档失效。

## 5. 停止、迁移与清理

临时停止只作用于当前 PFB：

```bash
make -C "$TASK_RETROM_ROOT" pfb-down PFB="$PFB"
```

旧命名卷环境在停止后、首次 build 前执行 `pfb-migrate-storage PFB=<name> CONFIRM=<actual-id>`，验证复制并原子发布 workspace，保留源卷；不要同时做首次基座导入。兼容 migration 原地执行。开发分支明确引入不兼容数据变化时，在 app 和数据库都停止后执行 `pfb-data-reset PFB=<name> CONFIRM=<actual-id>`，将 `data/` 与 `postgres/` 归档到 `.pfb/workspace/reset-backups/<timestamp>/`，保留 Provider、依赖缓存、ID 和 URL，并记录返回的归档路径。

完整下线并删除 PFB 数据及源码工作树时，从 **retrom-project 根目录**执行：

```bash
make pfb-remove PFB="$PFB"
```

工作区先检查全部注册源码工作树 clean，再显示实际 ID、数据及精确删除范围，交互确认后才销毁。它保留本地分支及共享网关／下载缓存；不要用 Retrom 的同名底层命令替代这套完整清理，也不要强制删除脏 worktree。

工程与浏览器回归见[质量与测试](engineering-quality-and-testing.md)，记录要求见[项目验收](project-acceptance.md)。
