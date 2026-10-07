# Retrom 实施规范

本文件适用于全仓库，子规范只能补充或收紧。用户明确授权与会话约束优先；事实冲突先核对并修正，不能静默保留两套契约。

开始工作按范围阅读docs/README.md、retrom-product-architecture.md、data-model.md、http-api-contract.md、engineering-quality-and-testing.md及project-acceptance.md。API权威是api/openapi.yaml及Provider V1 schemas；运行事实归配套retrom-runtime。正式文档不依赖临时设计目录。

## 仓库与架构

Go位于cmd/internal/migrations，Web位于web，契约位于api，长期文档位于docs。workspace/manifest.yaml拥有依赖仓库和维护branch。每个仓库独立Git；PFB内修改本PFB源码，禁止通过旧checkout或另一PFB源码参与执行。

cmd/retrom只组装资源与生命周期。HTTP处理认证、验证与响应，只调用service，不能越过公开Repository读取SQL。model/format是纯事实与解析；persistence拥有SQL和短事务；storage拥有POSIX文件；temporary拥有窄Redis能力；runtimeclient是唯一Host进程调用边界。基础层不依赖业务，runtime/存档不依赖导入服务。

产品八模块为游戏库（含收藏）、存档、最近、BIOS、目录、审核（含游戏导入）、标签、用户。扫描进度、运行和文件维护不发展成第九个业务中心。只保存19业务表当前事实，禁止FK/CHECK/TRIGGER/VIEW以及通用Job/Event/Input/Lease模型。目录和核心关系不seed。

引擎/入口/BIOS/DAT/Parent/指纹/checkpoint规则归runtime；Host不复制规则或猜Target分支。资料与替换只验证领域结构/引用及身份，不把完整Prepare当运行校验门禁。没有旧数据、旧接口兼容分支，不因重构升协议代际。

## 文件、事务与授权

来源root只读，所有运行使用受管副本。IO/Node准备事务外，短事务内只读写必要领域事实；Game+成功进度、BIOS+成功进度原子提交。提交结果不明按本次分配ID核对，不盲目重做。保存锁Game确认published后CAS；目录按普通查询允许已明确的低概率竞态，不扩成锁协议。

User是唯一私有主体。管理员不越权读取其他人的save/favorite/recent。Origin精确保护、已登录CSRF、可信代理CIDR、密码blocklist/围栏、会话撤销和一次消费链接必须保留。Redis丢失不会注销PG身份；退出不清除未同步浏览器草稿。

先写/fsync/rename新文件，再短事务切换active，退休文件宽限24h。清理有界、复查当前引用、失败保留deleted，不能吞掉错误或以文件系统扫描重建业务事实。私有素材和路径/账号凭据/会话不提交。

## 工程质量

所有Go/Web/runtime原lint、格式、类型与复杂度阈值保持强度，更新新路径使实际检查有效。禁止删除门禁、降低阈值、扩大排除、无理由nolint或机械拆字绕过复杂度。Go生产1000行/测试1200行，Web生产600行/测试800行，CSS800行；生成文件只有严格指定目录且生成标记正确才豁免。

常规检查make backend-check/web-check/api-check；真实PG窗口make integration-test，必须显式独立数据库，不能skip。关键风险测试覆盖失败回滚、提交结果核对、保存并发CAS/删除围栏、BIOS并发不覆盖、文件移除失败/引用保护及运行IPC取消。普通低影响改动按实际风险选测试，不复制实现制造覆盖数字。

发现bug在最近确定性边界固化回归；涉及浏览器或私有核心内容时补实际产品记录。核心声明、ready、单张标题图均不是完整运行通过。浏览器以正常API导入合法fixture并验证画面/输入/保存/新实例恢复，保留所有视口和UI规则。

数据隔离上限REPEATABLE READ，测试源码不豁免结构规则。最终源码全量门禁及T01–T47必须按实际证据汇总，未完成/环境不足分开列出，不把里程碑当整体完成。提交、push、PR、tag、发布遵循会话授权及协议预检，不自动执行。
