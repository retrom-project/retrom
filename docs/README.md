# Retrom 文档

当前实现采用八个产品模块、19张业务表、统一 User 身份和运行组件事实源。数据库与 HTTP 契约直接更新；本分支没有旧数据或旧接口兼容层，Provider Module V1 的代际保持不变。

| 事实源 | 内容 |
| --- | --- |
| [Docker 部署](docker-deployment.md) | 发行镜像、域名与 HTTPS、持久存储、启动检查及升级 |
| [产品与模块](retrom-product-architecture.md) | 范围、职责、依赖方向与写入边界 |
| [数据模型](data-model.md) | 19表、状态、索引与应用完整性 |
| [HTTP](http-api-contract.md) / [OpenAPI](../api/openapi.yaml) | 权限、请求、错误和唯一字段定义 |
| [导入审核](import-and-review.md) | 两个服务器来源、原子进度与同一 Game 审核 |
| [运行与存档](runtime-and-play-data.md) | runtime 工具、冻结身份、资源与恢复 |
| [存储](storage-and-database.md) | 受管所有权、文件发布、宽限清理 |
| [账号与部署](backend-api-and-operations.md) | 登录保护、配置、PostgreSQL/Redis/镜像 |
| [质量](engineering-quality-and-testing.md) | 有效门禁及风险测试 |
| [依赖输入](dependency-management.md) | 唯一配套描述、归档认证、运输与发行 |
| [验收](project-acceptance.md) | 完整检查项、证据、性能和未完成边界 |
| [PFB](pfb-development.md) | 隔离工作树、持久状态与不可变工具 |
| [实施状态](implementation-plan.md) | 分阶段门禁与本次可验证状态 |

API字段以当前OpenAPI及Provider V1 schema为准；运行规则、核心声明和指纹由retrom-runtime拥有。正式文档不依赖临时设计目录。验收记录和私有素材位于忽略的验收workspace，不能把历史截图或运行时声明数量当作当前产品通过证据。
