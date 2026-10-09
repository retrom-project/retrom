# Retrom

Retrom 是面向个人和可信朋友的自托管浏览器游戏库。管理员导入游戏、整理资料和运行依赖，用户在浏览器中游玩，并管理自己的存档、收藏和最近游玩记录。

支持普通界面和手柄沉浸界面。平台、核心和游戏格式由配套的 [retrom-runtime](https://github.com/retrom-project/retrom-runtime) 提供，实际验证范围与已知限制见[验收记录](docs/project-acceptance.md)。

## 主要功能

| 功能 | 说明 |
| --- | --- |
| 游戏库 | 游戏详情、搜索筛选、封面与视频、共享标签 |
| 浏览器游玩 | 在浏览器中启动游戏，使用普通界面或手柄沉浸界面 |
| 存档 | 即时存档、游戏原生存档、重命名与恢复；具体能力取决于核心 |
| 收藏与最近游玩 | 每个用户独立管理收藏、收藏夹和最近游玩记录 |
| 游戏导入 | 扫描服务器上的 Pegasus / EmulationStation 来源，审核、试玩后发布 |
| 目录与运行依赖 | 配置平台和默认核心，安装 BIOS，查看并补齐缺失依赖与街机父包 |
| 用户管理 | 首次管理员初始化、账号管理、邀请和密码重置 |

每个游戏条目对应一份独立作品内容。游戏导入通过服务器目录扫描完成，管理员也可替换已入库游戏的文件、封面和视频。

## Docker 部署

使用 Linux 服务器、Docker Engine 和 Compose 部署。发行镜像已包含应用及配套运行资源，游戏和 BIOS 由管理员自行准备。

1. 从 [Releases](https://github.com/retrom-project/retrom/releases) 选择发行版本。
2. 按照 [Docker 部署指南](docs/docker-deployment.md) 配置域名、HTTPS、数据目录并启动服务。
3. 打开 `https://你的域名/setup` 创建首个管理员。
4. 在“游戏目录”创建目录，再从“游戏入库 → 来源扫描”导入游戏，审核并发布。

首次安装、版本兼容、备份和升级的具体步骤均见部署指南。

## 开发

后端使用 Go，前端使用 Next.js / TypeScript。持久数据存放在 PostgreSQL 中，临时运行状态由 Redis 管理；游戏运行与核心适配由 retrom-runtime 提供。

联合开发使用 [retrom-project](https://github.com/retrom-project/retrom-project) 工作区中的命名 PFB。环境准备和日常开发流程见 [PFB 开发说明](docs/pfb-development.md)，检查命令和测试要求见[工程质量与测试](docs/engineering-quality-and-testing.md)。贡献代码前请阅读 [AGENTS.md](AGENTS.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [Docker 部署指南](docs/docker-deployment.md) | 安装配置、启动检查、数据备份、升级与故障排查 |
| [文档索引](docs/README.md) | 产品、API、数据模型及工程文档的完整入口 |
| [导入与审核](docs/import-and-review.md) | 来源扫描、游戏审核和内容维护 |
| [账号、配置与部署](docs/backend-api-and-operations.md) | 服务配置、账号机制和代理要求 |
| [配套依赖输入与发布](docs/dependency-management.md) | runtime、Provider 和镜像的配套关系 |
| [验收记录](docs/project-acceptance.md) | 实际验证范围与已知限制 |

本项目不附带商业游戏或 BIOS。
