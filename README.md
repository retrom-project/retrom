# Retrom

[![Retrom：自托管多平台浏览器游戏库][promotion-preview]][promotion-original]

Retrom 是面向个人和可信朋友的自托管多平台浏览器游戏库。集中整理游戏，直接在浏览器中游玩，随时从存档继续进度。

管理员维护共享游戏库，每个用户拥有独立的存档、收藏夹和最近游玩记录。

[快速部署](#快速部署) · [功能预览](#功能预览) · [支持平台](#支持平台) · [文档](#文档)

[![Retrom 首页：纯白之夏主视觉、多平台最近游玩和个人收藏][home-preview]][home-original]

## 功能预览

### 浏览游戏，直接开玩

通过封面、游戏详情、平台分类、共享标签和搜索筛选找到想玩的游戏，在浏览器中启动。支持普通界面和手柄沉浸界面。

[![游戏库：封面墙、平台分类、搜索和筛选][library-preview]][library-original]

### 保存进度，随时继续

支持即时存档与游戏原生存档，提供截图预览、重命名、搜索筛选和恢复入口，方便找到想要继续的进度。

[![我的存档：索尼克2的多份即时存档、截图、名称和继续游玩入口][saves-preview]][saves-original]

### 收藏分类与最近游玩

按平台、系列或主题建立收藏夹，在首页查看最近玩过和一直喜欢的游戏。

[![我的收藏：跨平台游戏封面和收藏夹分类导航][favorites-preview]][favorites-original]

<details>
<summary>更多界面预览</summary>

首页：

[![Retrom 首页：索尼克2的存档主视觉和多平台游戏记录][sonic-preview]][sonic-original]

最近游玩：

[![最近游玩：按时间排列的游戏记录及开始游戏入口][recent-preview]][recent-original]

</details>

## 支持平台

支持 NES、SNES、Mega Drive、GBA、Nintendo DS、街机，以及 RPG Maker、ONScripter、Siglus 等引擎游戏。平台与核心由配套的 [retrom-runtime](https://github.com/retrom-project/retrom-runtime) 提供，具体游戏格式、输入方式和存档类型随核心而异。

## 快速部署

准备 Linux 服务器、Docker Engine 和 Compose，以及应用域名、游戏隔离泛域名和覆盖两者的 HTTPS 证书。发行镜像已包含应用及配套运行资源，游戏和 BIOS 由管理员自行准备。

1. 从 [Releases](https://github.com/retrom-project/retrom/releases) 选择发行版本。
2. 按照 [Docker 部署指南](docs/docker-deployment.md) 获取该版本的 Compose 和 Nginx 配置，设置域名、HTTPS、持久数据目录并启动服务。
3. 打开 `https://你的域名/setup` 创建首个管理员。
4. 在“游戏目录”创建目录，再从“游戏入库 → 来源扫描”导入游戏，审核并发布。

首次安装、版本兼容、备份和升级的具体步骤均见部署指南。

## 管理自己的游戏库

管理员通过服务器目录扫描导入 Pegasus / EmulationStation 来源，整理资料、试玩审核后发布，也可替换已入库游戏的文件、封面和视频。

| 管理能力 | 用途 |
| --- | --- |
| 目录与核心 | 配置平台目录、可用核心和默认核心 |
| 运行依赖 | 安装 BIOS，查看并补齐缺失依赖与街机父包 |
| 资料与标签 | 维护游戏详情、封面、视频和共享标签 |
| 账号管理 | 管理用户、邀请和密码重置 |

## 文档

| 文档 | 内容 |
| --- | --- |
| [Docker 部署指南](docs/docker-deployment.md) | 安装配置、启动检查、数据备份、升级与故障排查 |
| [文档索引](docs/README.md) | 使用、管理和开发文档的完整入口 |
| [导入与审核](docs/import-and-review.md) | 来源扫描、游戏审核和内容维护 |
| [运行与存档](docs/runtime-and-play-data.md) | 游戏运行、存档保存与恢复 |
| [账号、配置与部署](docs/backend-api-and-operations.md) | 账号管理、服务配置和反向代理 |

## 开发

后端使用 Go，前端使用 Next.js / TypeScript。持久数据存放在 PostgreSQL 中，临时运行状态由 Redis 管理；游戏运行与核心适配由 retrom-runtime 提供。

联合开发使用 [retrom-project](https://github.com/retrom-project/retrom-project) 工作区中的命名 PFB。环境准备和日常开发流程见 [PFB 开发说明](docs/pfb-development.md)，检查命令和测试要求见[工程质量与测试](docs/engineering-quality-and-testing.md)。贡献代码前请阅读 [AGENTS.md](AGENTS.md)。

本项目不附带商业游戏或 BIOS。

[home-preview]: https://raw.githubusercontent.com/retrom-project/retrom-images/014c6fccd243d8a57e50876707dc49bf53eb9b74/showcase/2026-10-18-v0.0.133/03-home-purewhite.webp
[home-original]: https://github.com/retrom-project/retrom-images/releases/download/showcase-2026-10-18-v0.0.133/03-home-purewhite.png
[library-preview]: https://raw.githubusercontent.com/retrom-project/retrom-images/014c6fccd243d8a57e50876707dc49bf53eb9b74/showcase/2026-10-18-v0.0.133/10-library-overview.webp
[library-original]: https://github.com/retrom-project/retrom-images/releases/download/showcase-2026-10-18-v0.0.133/10-library-overview.png
[saves-preview]: https://raw.githubusercontent.com/retrom-project/retrom-images/014c6fccd243d8a57e50876707dc49bf53eb9b74/showcase/2026-10-18-v0.0.133/21-saves-sonic.webp
[saves-original]: https://github.com/retrom-project/retrom-images/releases/download/showcase-2026-10-18-v0.0.133/21-saves-sonic.png
[favorites-preview]: https://raw.githubusercontent.com/retrom-project/retrom-images/014c6fccd243d8a57e50876707dc49bf53eb9b74/showcase/2026-10-18-v0.0.133/30-favorites-all.webp
[favorites-original]: https://github.com/retrom-project/retrom-images/releases/download/showcase-2026-10-18-v0.0.133/30-favorites-all.png
[sonic-preview]: https://raw.githubusercontent.com/retrom-project/retrom-images/014c6fccd243d8a57e50876707dc49bf53eb9b74/showcase/2026-10-18-v0.0.133/02-home-sonic.webp
[sonic-original]: https://github.com/retrom-project/retrom-images/releases/download/showcase-2026-10-18-v0.0.133/02-home-sonic.png
[recent-preview]: https://raw.githubusercontent.com/retrom-project/retrom-images/014c6fccd243d8a57e50876707dc49bf53eb9b74/showcase/2026-10-18-v0.0.133/40-recent-overview.webp
[recent-original]: https://github.com/retrom-project/retrom-images/releases/download/showcase-2026-10-18-v0.0.133/40-recent-overview.png
[promotion-preview]: https://raw.githubusercontent.com/retrom-project/retrom-images/014c6fccd243d8a57e50876707dc49bf53eb9b74/showcase/2026-10-18-v0.0.133/retrom-promotion-20261018.webp
[promotion-original]: https://github.com/retrom-project/retrom-images/releases/download/showcase-2026-10-18-v0.0.133/retrom-promotion-20261018.png
