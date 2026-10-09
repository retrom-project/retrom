# Retrom

Retrom 是面向个人和可信朋友的自托管浏览器游戏库。管理员从服务器导入游戏、整理目录和运行依赖；用户在浏览器中游玩，管理自己的存档、收藏和最近游玩记录。支持普通界面和手柄沉浸界面，具体核心与游戏格式由配套的 [retrom-runtime](https://github.com/retrom-project/retrom-runtime) 提供。

本次重构面向全新部署，**不兼容旧版数据库、存档、受管文件和接口，也不提供历史数据迁移**。请使用新的数据库和数据目录。各平台的实际验证范围及已知限制见[验收记录](docs/project-acceptance.md)，核心列表不等于所有游戏均已验证。

## 功能

| 模块 | 能力 |
| --- | --- |
| 游戏库 | 游戏详情、搜索筛选、封面与视频、个人收藏和收藏夹 |
| 存档 | 即时存档或游戏原生存档、重命名、恢复及兼容性提示；具体语义取决于核心 |
| 最近游玩 | 按当前用户最后成功游玩的时间排列游戏 |
| 运行依赖 | BIOS 等文件的安装、替换、缺失要求查看与服务器扫描 |
| 游戏目录 | 每个目录选择一个平台、可用核心和默认核心，可一键创建推荐目录 |
| 游戏审核 | Pegasus / EmulationStation 服务端导入、扫描进度、统一待审列表、试玩与首次发布 |
| 标签 | 共享标签、推荐标签与游戏分类 |
| 用户 | 首次管理员初始化、登录、账号管理、邀请和密码重置 |

一个游戏条目对应一份独立作品内容，不关联不同地区版本，不支持多盘切换。游戏包、封面及视频可在管理页面替换；街机缺少 Parent ROM 时可查看所需父包并上传补齐。游戏仅首次入库需要审核，已发布内容的后续替换不重新审核。

游戏导入只支持服务器扫描，不提供浏览器批量上传游戏或在线刮削。BIOS、媒体和父包等管理文件仍可上传。存档按保存时的核心实现指纹与完整游戏内容 hash 判断兼容性；不匹配的存档仍可查看，但不能继续游玩。

## 部署

### 环境与镜像

需要 Linux 本地持久文件系统、Docker Engine / Compose，以及可配置 DNS 和 TLS 的反向代理。部署包含 PostgreSQL、Redis、Retrom 后端、Web 前端和代理五个服务。

从 [Releases](https://github.com/retrom-project/retrom/releases) 选择同一发行版本的两个镜像：

- `xxxsen/retrom:<版本标签>`：Go 后端、离线 runtime 工具和已验证 Provider。
- `xxxsen/retrom-web:<版本标签>`：Next.js 前端。

候选版使用带 `-rc.N` 的明确标签，前后端需选同一标签；RC 不更新镜像的 `latest`。

镜像已包含运行所需的程序依赖，不需要在服务器另外克隆或构建核心。游戏和 BIOS 由管理员自行提供。不要将本次重构的前端与旧版后端混用。

### 配置并启动

1. 获取所选版本对应的本仓库文件，参考 [Compose 示例](docker/docker-compose.yml.example) 和 [Nginx 路由示例](docker/nginx.conf.example) 修改配置。示例的 Nginx 只展示 HTTP 路由，须在对外使用前配置 HTTPS 和证书。
2. 准备宿主域名，例如 `games.example.com`，以及隔离游戏域名 `*.games.example.com`，为两者配置 DNS 与 TLS。同步修改 Compose 中的 `RETROM_PUBLIC_ORIGIN`、`RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE` 和 Nginx 的域名。
3. 创建应用持久目录，例如 `/srv/retrom/data`，确保容器的 UID/GID `1000:1000` 可读写。修改只读来源挂载，例如 `/srv/roms:/server-data:ro`；管理员在页面中选择的是容器里的 `/server-data`。
4. 在仓库根目录创建私有 `.env`，填写实际版本和数据库凭据：

   ```dotenv
   RETROM_VERSION=<所选Release的完整标签>
   RETROM_POSTGRES_PASSWORD=<自行生成的数据库密码>
   RETROM_DATABASE_URL=postgres://retrom:<URL编码后的同一密码>@postgres:5432/retrom?sslmode=disable
   ```

   不要提交该文件。数据库密码中的特殊字符需要在连接 URL 中编码；如果更改服务名称或数据库名称，也要同步修改连接 URL。示例连接仅用于 Compose 内部网络，远程数据库应按其实际要求配置 TLS。

5. 从仓库根目录检查并启动：

   ```sh
   docker compose --env-file .env -f docker/docker-compose.yml.example config --quiet
   docker compose --env-file .env -f docker/docker-compose.yml.example pull
   docker compose --env-file .env -f docker/docker-compose.yml.example up -d
   docker compose --env-file .env -f docker/docker-compose.yml.example logs --tail=100 retrom retrom-web
   ```

6. 打开 `https://你的域名/setup`，创建首个管理员。空实例没有默认生产账号；生产环境不要设置 `RETROM_MODE=test`。

代理需将 `/api/v1` 和 `/runtime` 转给后端，其余宿主页面转给 Web。隔离域只转发 `/__retrom/runtime-isolation/`，其他路径返回 404；不能把所有请求都交给前端。后端默认信任内网或回环地址的直接代理，无需配置代理网段；nginx 必须覆盖 `X-Forwarded-For`。完整配置说明见[账号、配置与部署](docs/backend-api-and-operations.md)。

### 首次导入

1. 在“游戏目录”创建目录，选择平台与核心；也可使用“一键创建推荐目录”。目录和核心关系不会由数据库迁移自动填充。
2. 在“游戏入库 → 来源扫描”选择 Pegasus 或 EmulationStation 来源，浏览服务器目录，核对集合与目标游戏目录后开始扫描。页面进入待审核列表并展示当前扫描进度。
3. 在待审核详情补充资料、标签与运行配置，查看 BIOS 缺项；街机父包缺项在运行配置中补齐。通过并发布后进入下一条待审。
4. 在“运行依赖”上传所需 BIOS；批量 BIOS 扫描入口及进度同样位于“来源扫描”。

管理员可以浏览服务进程可访问的目录；容器之外的宿主目录必须先挂载。导入会将内容复制到 Retrom 受管目录，来源始终只读，完成后移走源文件不影响已导入游戏。快速审批只检查必需 BIOS 条件，不代替实际试玩或完整运行验证。

### 持久化与维护

PostgreSQL 保存账号和业务数据；`/var/lib/retrom` 保存游戏、BIOS、存档、媒体及账号链接密钥，备份时需要同时覆盖两者。Redis 仅保存有有效期的运行上下文和限流状态，示例关闭 Redis 持久化；重启 Redis 会影响正在进行的运行会话，但它不是业务数据的备份来源。

游戏删除先软删除，再由维护任务清理文件和关联数据；替换文件写入新路径后切换引用，旧文件保留 24 小时宽限再回收。不要直接清空受管目录以替代管理操作。详细规则见[存储与数据库](docs/storage-and-database.md)。

后续更新需先阅读对应 Release 的不兼容说明，备份数据库和应用持久目录，再将 `RETROM_VERSION` 改为目标标签并执行上述 `pull` / `up -d`。前后端始终使用同一发行版本。

## 开发与验证

后端使用 Go，前端使用 Next.js / TypeScript，PostgreSQL 保存持久事实，Redis 保存临时状态。运行配置解释、核心适配、BIOS / Parent 要求和存档兼容判断由 retrom-runtime 统一提供。业务层分为八个模块，使用 19 张业务表，不引入数据库外键、CHECK、trigger 或 view。

联合开发使用 `retrom-project` 工作区中的命名 PFB，将 Retrom 与 runtime 放在同一工作树下。先阅读 [PFB 开发说明](docs/pfb-development.md) 和各仓库的 `AGENTS.md`；工作区根目录可用以下命令查看、准备源码：

```sh
make pfb-list
make init PFB=my-feature REPOS="retrom-runtime"
```

随后按 PFB 流程初始化、导入已验证 Provider 基座、构建并启动。PFB 使用专属 `*.localhost:3000` 地址；普通开发入口使用 `localhost:4000`。每日 Web 修改使用 HMR，Go 修改重启对应 PFB；不要为日常重启重建核心或清空数据。

工具链版本以 [go.mod](go.mod)、[.node-version](.node-version) 和锁文件为准。在 Retrom 仓库中执行基础检查：

```sh
make prepare-deps
make backend-check web-check api-check
```

真实 PostgreSQL 集成测试必须配置独立测试数据库连接 `RETROM_TEST_DATABASE_URL`，并提供与当前 `data/runtime-inputs.json` 配套的 runtime 工具和 Provider；可用 `python3 scripts/prepare_image_inputs.py --run make integration-test` 完成准备并运行。UI 修改另需完整 `ACC-RF-BROWSER` 浏览器验收。具体准备步骤、证据范围及命令见[工程质量与测试](docs/engineering-quality-and-testing.md)。

## 文档

- [文档索引](docs/README.md)
- [产品与模块边界](docs/retrom-product-architecture.md)
- [数据库设计](docs/data-model.md)
- [HTTP API 契约](docs/http-api-contract.md)
- [配套 runtime 输入与发布](docs/dependency-management.md)
- [UI 规范](docs/ui-specification.md)
- [验收标准与已知限制](docs/project-acceptance.md)

本项目不附带商业游戏或 BIOS；请仅导入你有权使用的内容。
