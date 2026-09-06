# Retrom

**把自己的游戏库搬进浏览器。**

Retrom 是一个面向个人和可信朋友的自托管游戏平台。集中管理游戏、封面和元信息，打开浏览器就能找游戏、开始游玩，或从上次保存的进度继续。游戏库由你维护，每个账号拥有独立的存档、收藏和游玩记录。

[快速开始](#快速开始) · [部署指南](docs/backend-api-and-operations.md) · [文档](docs/README.md) · [反馈问题](https://github.com/retrom-project/retrom/issues) · [参与贡献](#参与贡献)

> 项目正在持续开发，当前正式支持 Chrome。平台、核心与游戏的兼容范围见[运行时验证基线](docs/core-runtime-validation.md)。

## 功能

- **找游戏与继续游玩**：按标题搜索，按平台、目录和标签筛选；查看最近游玩，从首页或存档页继续进度。
- **浏览器内运行**：通过 EmulatorJS 和 retrom-runtime 接入多种主机、街机及 RPG Maker、ONScripter、KiriKiri 等游戏项目，提供全屏、输入控制和受支持的存档能力。
- **适合不同设备的界面**：PC 提供完整的浏览与管理界面；手机围绕“首页、游戏库、我的”简化触屏操作；独立沉浸模式面向电视和标准手柄。
- **整理已有收藏**：支持文件、目录、压缩包和受支持的多盘内容，也可从 Pegasus、EmulationStation 游戏库导入；通过内容哈希去重，审核后发布。
- **与朋友共享**：通过邀请加入同一游戏库，账号间的收藏、存档和游玩记录彼此独立。
- **可选双人联机**：为已支持的核心配置提供异地联机，生产环境默认关闭；支持范围和运行要求见[运行与游玩数据](docs/runtime-and-play-data.md)。

## 快速开始

以下步骤在本机启动开发测试实例，适合体验和参与开发。正式服务请使用[自托管部署](#自托管部署)中的配置。

### 环境要求

- Linux x86-64（含 WSL2）。
- Git、Make、Python 3、`curl`、`tar`、`xz`。
- 支持 C++20 的 `g++`，以及 `7z` 或 `7zz`。
- 首次准备依赖时可访问互联网。

项目命令会下载并校验固定版本的 Go、Node.js、Chrome for Testing 和运行时依赖，后续复用本地缓存。请使用普通用户运行开发命令。

### 启动

```bash
git clone https://github.com/retrom-project/retrom.git
cd retrom
make install-deps
make dev
```

打开 [http://localhost:4000](http://localhost:4000)，使用开发测试账号 `test` / `test` 登录。

`make dev` 在宿主机启动前后端，不需要 Docker。测试数据默认保存在 `.dev-data/data/`，按 `Ctrl+C` 停止服务后仍会保留。该模式只用于本机测试，不应作为公网服务。

### 添加第一款游戏

1. 在电脑上进入管理后台，在“游戏目录”页补齐推荐模板或创建自己的目录。
2. 通过浏览器上传游戏文件、目录或压缩包；已有 Pegasus / EmulationStation 游戏库时，也可使用“本地扫描”导入服务器上的内容。
3. 在审核页确认元信息、运行方式及所需依赖，发布后即可在游戏库找到并启动。

Retrom 不附带商业游戏或 BIOS，请导入你有权使用的内容。平台所需的文件格式、BIOS 和街机依赖见[导入与审核](docs/import-and-review.md)及 [BIOS 与 Arcade 说明](docs/bios-and-arcade.md)。

只想游玩时，使用 Chrome 打开管理员提供的站点地址，接受邀请并登录即可；手机进入游戏时需要横屏。导入、审核和其他管理操作在电脑上完成。

## 自托管部署

Retrom 由 Go 后端与 Next.js 前端组成，可构建为两个 OCI / Docker 镜像：

```bash
make build-images
```

| 镜像 | 职责 | 内部端口 |
| --- | --- | --- |
| `retrom:latest` | API、后台任务、数据存储与游戏运行资源 | `8080` |
| `retrom-web:latest` | Web 界面与 Player | `3000` |

该命令只构建镜像。当前需自行提供 Compose、Kubernetes 或其他部署编排，并配置 HTTPS 反向代理及可写的持久数据目录 `RETROM_DATA_DIR`。RPG Maker MV/MZ 等隔离运行入口还需要配置运行时子域名与证书。

全新生产实例通过主机上的 `retrom setup-code` 与网页 `/setup` 创建首位管理员。完整配置、路由及初始化步骤见[后端与部署说明](docs/backend-api-and-operations.md)；长期运行前请按[存储、备份与恢复](docs/storage-and-database.md)准备备份。

## 开发

后端使用 Go 与 SQLite，前端使用 Next.js、React 和 TypeScript。游戏运行能力由独立的 [retrom-runtime](https://github.com/retrom-project/retrom-runtime) 和 EmulatorJS Provider 提供。

```text
cmd/retrom/    服务与管理命令入口
internal/     后端业务、HTTP 与存储实现
migrations/   数据库迁移
web/          Web 界面、Player 与前端测试
api/          OpenAPI 契约
docs/         产品、开发、部署与验收文档
```

| 命令 | 用途 |
| --- | --- |
| `make dev` | 启动本机开发服务 |
| `make web-check` | 前端 lint、类型检查、单元测试与构建 |
| `make backend-check` | 后端格式、构建、测试与 lint |
| `make acceptance-case CASE=<Case ID>` | 运行指定的产品验收用例 |
| `make ci` | 运行仓库完整质量门禁 |
| `make deps-check` | 离线校验已准备的运行时依赖 |

需要同时开发多个分支或联调 runtime / core 时，使用 [PFB 开发流程](docs/pfb-development.md)。每个功能分支拥有独立 worktree、持久数据和稳定的本地访问地址。

## 文档

| 主题 | 入口 |
| --- | --- |
| 产品范围与架构 | [产品与架构总览](docs/retrom-product-architecture.md) |
| 导入、整理与发布 | [导入与审核](docs/import-and-review.md) |
| 游戏运行与存档 | [运行与游玩数据](docs/runtime-and-play-data.md) |
| 平台兼容与依赖 | [核心运行时验证](docs/core-runtime-validation.md) · [依赖管理](docs/dependency-management.md) |
| 测试与验收 | [工程质量与测试](docs/engineering-quality-and-testing.md) · [产品验收](docs/project-acceptance.md) |
| 完整文档地图 | [文档索引](docs/README.md) |

## 参与贡献

欢迎通过 [Issue](https://github.com/retrom-project/retrom/issues) 反馈问题、讨论功能，或提交 [Pull Request](https://github.com/retrom-project/retrom/pulls) 改进代码与文档。

报告问题时，请提供复现步骤、预期与实际结果、Retrom 和 Chrome 版本，以及相关平台、核心或错误码。示例尽量使用仓库中的公开测试程序，提交前移除凭据和私人游戏数据。

修改前请阅读 [AGENTS.md](AGENTS.md) 与相关领域文档；较大的调整先通过 Issue 说明方案。提交 PR 时描述改动的目的与验证结果，并按[测试规范](docs/engineering-quality-and-testing.md)运行受影响的检查；行为变化应同步对应文档。

## 致谢

Retrom 的浏览器运行能力建立在 EmulatorJS、libretro 生态与各引擎上游项目的工作之上。依赖来源、固定版本和许可材料由项目清单管理，详见[依赖管理](docs/dependency-management.md)。
