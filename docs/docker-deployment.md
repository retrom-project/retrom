# Docker 部署指南

[返回项目首页](../README.md) · [文档索引](README.md)

本文使用发行镜像与 Docker Compose 部署 Retrom，包含 PostgreSQL、Redis、Retrom 后端、Web 前端和 Nginx 五个服务。配置以所选版本的 [Compose 示例](../docker/docker-compose.yml.example)和 [Nginx 示例](../docker/nginx.conf.example)为准。

## 1. 准备环境

准备一台使用 Linux 本地持久文件系统的服务器，并安装 Docker Engine、Compose 插件、curl 和 OpenSSL。Docker 安装方式见[官方安装文档](https://docs.docker.com/engine/install/)及 [Compose 插件安装说明](https://docs.docker.com/compose/install/linux/)。

```bash
docker version
docker compose version
```

当前用户需能访问 Docker。本文将部署文件放在 `~/retrom-deploy`，与开发工作区分开保存。

准备以下域名及证书，将示例域名替换为实际域名：

| 用途 | 示例 | 要求 |
| --- | --- | --- |
| 应用入口 | `games.example.com` | DNS 指向服务器 |
| 游戏隔离子域名 | `*.games.example.com` | 泛域名 DNS 指向同一服务器 |
| HTTPS 证书 | 同时覆盖以上两者 | 完整证书链及对应私钥 |

证书必须同时包含应用域名和泛域名；单独的 `*.games.example.com` 证书不覆盖 `games.example.com`。证书配置说明见 [Nginx HTTPS 文档](https://nginx.org/en/docs/http/configuring_https_servers.html)。

当前版本使用 PostgreSQL 和新的受管数据结构，不兼容旧版数据库、存档和受管文件，也不提供历史数据迁移。首次安装请使用新的数据库和数据目录。

## 2. 选择版本并获取部署文件

从 [Releases](https://github.com/retrom-project/retrom/releases) 选择版本，将下方 `your-release-tag` 替换为完整发行标签。前后端镜像必须使用同一标签，部署文件也取自该版本。候选版使用带 `-rc.N` 的完整标签。

```bash
mkdir -p "$HOME/retrom-deploy"
cd "$HOME/retrom-deploy"

RETROM_RELEASE=your-release-tag
curl --fail --location \
  "https://raw.githubusercontent.com/retrom-project/retrom/${RETROM_RELEASE}/docker/docker-compose.yml.example" \
  --output compose.yaml
curl --fail --location \
  "https://raw.githubusercontent.com/retrom-project/retrom/${RETROM_RELEASE}/docker/nginx.conf.example" \
  --output nginx.conf

mkdir -p data certs
```

两张应用镜像分别为 `xxxsen/retrom:<版本标签>` 和 `xxxsen/retrom-web:<版本标签>`。镜像已包含配套 runtime 工具及经过验证的 Provider，服务器无需另行克隆或构建核心。

后续命令均在 `~/retrom-deploy` 中执行。

## 3. 配置数据库凭据

以下命令用于首次部署，生成随机数据库密码并写入私有 `.env`。升级时沿用已有凭据。

```bash
umask 077
RETROM_DB_PASSWORD="$(openssl rand -hex 32)"
cat > .env <<EOF
COMPOSE_PROJECT_NAME=retrom
RETROM_VERSION=${RETROM_RELEASE}
RETROM_POSTGRES_PASSWORD=${RETROM_DB_PASSWORD}
RETROM_DATABASE_URL=postgres://retrom:${RETROM_DB_PASSWORD}@postgres:5432/retrom?sslmode=disable
EOF
unset RETROM_DB_PASSWORD
```

生成的十六进制密码可直接放入连接 URL。自行设置密码时，特殊字符需在 `RETROM_DATABASE_URL` 中进行 URL 编码，并与 `RETROM_POSTGRES_PASSWORD` 保持一致。该连接面向 Compose 内部数据库；远程数据库按实际要求配置 TLS。

`COMPOSE_PROJECT_NAME` 固定此部署的项目名称，后续升级沿用同一名称，以继续使用已有数据库卷。妥善保存 `.env`，不要将其提交到 Git。

## 4. 配置域名和持久目录

编辑下载的 `compose.yaml`：修改 `retrom.environment` 中的两个域名字段，替换 `retrom.volumes`、`nginx.volumes` 和 `nginx.ports`。原文件的镜像、数据库连接、Redis 地址、服务依赖和网络等其他字段继续保留。以下片段列出需要修改的字段，供编辑时对照：

```yaml
services:
  retrom:
    environment:
      RETROM_PUBLIC_ORIGIN: "https://games.example.com"
      RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE: "https://{runId}.games.example.com"
    volumes:
      - ./data:/var/lib/retrom
      - /srv/roms:/server-data:ro
  nginx:
    volumes:
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro
      - ./certs:/etc/nginx/certs:ro
    ports:
      - "443:443"
```

`data/` 是应用持久目录，保存游戏、BIOS、存档、媒体和账号链接密钥。确保容器的 UID/GID `1000:1000` 能读写它；目录权限不足时，由服务器管理员调整所有权或访问权限。

将 `/srv/roms` 替换为实际素材目录，并保持只读挂载。管理员在页面中选择的路径是容器内的 `/server-data`。没有素材来源目录时可删除这一行挂载，之后需要导入时再添加。PostgreSQL 使用示例中的 `postgres-data` 命名卷；数据库和 Redis 保持仅在容器网络内访问。

## 5. 配置 HTTPS 和反向代理

将完整证书链和私钥分别保存为 `certs/fullchain.pem`、`certs/privkey.pem`。文件必须实际存在，证书需覆盖第 1 步的两个域名。

下载的 `nginx.conf` 包含两个 `server` 块。在两个块中都将 `listen 80;` 替换为以下内容，并将所有 `games.example.com` 替换为实际域名：

```nginx
listen 443 ssl;
ssl_certificate /etc/nginx/certs/fullchain.pem;
ssl_certificate_key /etc/nginx/certs/privkey.pem;
```

保留模板的路由、上传限制和代理请求头。路由必须满足：

| 域名与路径 | 转发目标 |
| --- | --- |
| 应用域名的 `/api/v1`、`/runtime` 及其子路径 | `retrom:8080` |
| 应用域名的其他页面 | `retrom-web:3000` |
| 游戏隔离域名的 `/__retrom/runtime-isolation/` 前缀 | `retrom:8080` |
| 游戏隔离域名的其他路径 | 返回 404 |

代理覆盖 `X-Forwarded-For` 并传递实际的 HTTPS 协议信息；示例在 Nginx 内终止 TLS，原有 `X-Forwarded-Proto $scheme` 可直接保留。后端默认信任内网与回环地址的直接代理，无需另配代理网段。

```bash
chmod 600 .env certs/privkey.pem
```

开放服务器的 443 端口。本文通过 HTTPS 访问应用；如使用现有外部代理终止 TLS，按实际代理链配置转发头，确保主域名和隔离域名的路由均符合上表。

## 6. 检查并启动

```bash
docker compose --env-file .env config --quiet
docker compose --env-file .env pull
docker compose --env-file .env up -d
docker compose --env-file .env ps
docker compose --env-file .env logs --tail=100 retrom retrom-web nginx
```

`config --quiet` 校验配置且不输出展开后的数据库凭据，行为见 [Compose 配置命令说明](https://docs.docker.com/reference/cli/docker/compose/config/)。

PostgreSQL 和 Redis 的健康检查通过后，后端才会启动。检查 Nginx 配置及后端依赖就绪状态：

```bash
docker compose --env-file .env exec -T nginx nginx -t
docker compose --env-file .env exec -T retrom node -e \
  'fetch("http://127.0.0.1:8080/health/ready").then(r => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))'
```

健康接口仅用于内部依赖检查。继续打开实际域名，验证登录、导入和游戏游玩。

## 7. 创建管理员并导入游戏

打开 `https://你的域名/setup` 创建首个管理员。空实例没有默认生产账号，部署使用镜像默认的 release 模式。

1. 在“游戏目录”创建目录，选择平台、允许的核心和默认核心；也可使用“一键创建推荐目录”。
2. 在“游戏入库 → 来源扫描”选择 Pegasus 或 EmulationStation 来源，浏览容器内的素材目录，检查集合与目标目录后开始扫描。
3. 在待审核详情补充资料、查看 BIOS 和街机父包缺项，完成试玩后发布。
4. 在“运行依赖”上传所需 BIOS；批量 BIOS 扫描同样从“来源扫描”进入。

导入会将内容复制到受管目录，来源保持只读。游戏入库通过服务器扫描完成，BIOS、媒体和街机父包可在相应管理页面上传。

## 数据备份与版本升级

备份应同时包含 PostgreSQL 数据和 `data/` 目录，以及恢复部署所需的 `.env`、Compose、代理配置。暂停应用写入后，在同一备份窗口保存数据库与文件；恢复时使用这两部分对应的备份。

Redis 保存带有效期的运行上下文和限流状态，示例不启用持久化；重启会影响正在进行的运行会话。业务备份以 PostgreSQL 和受管目录为准。

升级前阅读目标发行版说明，完成备份，将 `.env` 中的 `RETROM_VERSION` 改为目标版本，并核对该版本的 Compose、Nginx 示例是否需要同步调整。保留已有凭据、`COMPOSE_PROJECT_NAME` 和持久存储。

```bash
docker compose --env-file .env config --quiet
docker compose --env-file .env pull
docker compose --env-file .env up -d
docker compose --env-file .env ps
```

前后端始终使用同一发行标签。应用会在启动时执行该版本的数据库迁移，回退前需核对数据库和文件是否仍与旧版本兼容。

停止服务可使用 `docker compose --env-file .env stop`；`down` 默认保留命名卷，而 `down --volumes` 会删除数据库卷，具体行为见 [Compose down 说明](https://docs.docker.com/reference/cli/docker/compose/down/)。

## 常见问题

| 现象 | 检查项 |
| --- | --- |
| 镜像拉取失败或提示标签不存在 | 核对发行标签、两张镜像是否已发布及 Docker Hub 连接 |
| 后端无法启动 | 查看后端、PostgreSQL 和 Redis 日志，核对密码与连接 URL |
| 写入数据提示权限不足 | 检查 `data/` 是否允许 UID/GID `1000:1000` 读写 |
| 登录正常但游戏隔离页面失败 | 核对泛域名 DNS、证书、隔离 origin 模板及 Nginx 路由 |
| 来源目录不可见 | 核对后端只读挂载，并在页面中使用容器内路径 |
| Nginx 无法启动或返回 502 | 检查证书路径、`nginx.conf` 挂载及两个应用服务的状态 |
| Docker 提示网段冲突 | 将 Compose 示例中的私有子网改为服务器上未占用的网段 |

更多服务参数和存储规则见[账号、配置与部署](backend-api-and-operations.md)及[存储与数据库](storage-and-database.md)。
