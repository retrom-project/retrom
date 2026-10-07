# Retrom

Retrom 是个人和可信朋友共享的自托管浏览器游戏库。管理员从受控服务器 Pegasus / EmulationStation 来源扫描和审核游戏，维护目录、媒体、标签与BIOS；每个账号拥有自己的存档、最近游玩、收藏和收藏夹。

当前工程使用Go、PostgreSQL、Redis、Next.js和配套retrom-runtime。19张业务表保存当前事实，Redis只保存有有效期的运行上下文与临时限流。运行解释、核心适配、资源桥和存档兼容由同版runtime提供；完整支持范围以真实产品矩阵为准。

开发与联调使用命名PFB，保持Retrom/runtime同树。显式准备依赖、不可变runtime工具与已验证Provider后执行pfb-up；日常restart不构建核心或Provider归档。新空生产实例通过/setup初始化管理员，测试模式只用于隔离开发。

- [完整文档](docs/README.md)
- [PFB开发](docs/pfb-development.md)
- [配置与部署](docs/backend-api-and-operations.md)
- [工程门禁](docs/engineering-quality-and-testing.md)
- [配套依赖输入](docs/dependency-management.md)
- [当前验收标准与完成边界](docs/project-acceptance.md)

后端与Web镜像分开构建，由版本控制的 `data/runtime-inputs.json` 固定配套runtime-tool及两个Provider。当前描述是未发布候选，准备时提供其中三个归档的本地运输目录或HTTPS根；完整集合经过认证后才进入镜像，缺少运输输入会明确失败。生产部署还需要PostgreSQL、Redis、可信反向代理、TLS及独立隔离origin。Retrom不附带商业游戏或BIOS；仅导入有权使用的内容，源目录只读，私有素材和路径不提交。
