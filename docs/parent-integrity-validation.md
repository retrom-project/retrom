# Parent 原始文件交付与 BIOS 扫描验证

本次在 `bios-import-recovery` PFB 修改 Retrom 与 retrom-runtime，沿用现有 Provider V1 和 Game 运行配置，没有数据库表、索引或 migration 变化。私有游戏和浏览器证据保存在该 PFB 的 `.pfb/workspace/acceptance/parent-integrity/`，不提交 ROM、凭据或部署数据。

## 修复边界

Parent 通过公共 `EXTERNAL_FILE_SET` 逐文件交付，每个资源保留原始 ZIP 的大小、SHA-256 和核心查询名称。单个与多个 Parent 使用相同路径，不合并 ZIP 成员，也不包装嵌套 ZIP。EmulatorJS 在启动前直接挂载原文件，MAME Current 写入 `/content/roms/<name>.zip`；独立 BIOS 按 DAT 要求另外提供。当前核心显式配置的 Parent 全部交付，不因入口 ZIP 看似自足而省略。切换 DAT 条目后的引用协调仍由原有通用流程负责。

删除旧 `PARENT_ARCHIVE` 资源和 CLI 扁平合并命令。MAME checkpoint 的 Parent 身份编码保持不变，原包字节相同时不会仅因传输方式变化失效。恢复历史被加工的 Parent 会改变游戏内容身份；已有存档保留，但不能伪造为新内容的可恢复存档。

BIOS 扫描修复 Host 转发校验要求时丢失 ZIP 成员事实的问题。正常扫描可识别匹配的原始 BIOS ZIP，仍只补齐缺失安装，不覆盖已有文件。

## 产品验证

| 范围 | 实际结果 |
| --- | --- |
| 线上 68 款历史 Parent 被加工的游戏及一个 MAME 故障样例 | 全部原样导入 PFB；69 次启动资源中的 ROM 与配置 Parent 摘要、大小匹配原始文件。EmulatorJS 同时核对核心文件系统内 ZIP 的实际字节 |
| 50 款原已发布游戏 | 原 Parent 恢复后均进入运行并响应输入 |
| MAME Current 的单 Parent 故障 | 原 ROM、Parent 和独立设备 BIOS 直接加载成功，实际投币、开始、移动及射击通过 |
| 18 款原待审核 FBNeo 游戏 | 原 Parent 字节正确，仍返回核心内容加载失败；保留原生缺件等诊断，不计为成功，不自动批准 |
| FBNeo、FBAlpha、MAME 2003、MAME 2003 Plus、MAME Current | 五核心手选 DAT、切换核心未选中状态、独立选择、共享 Parent 引用保留、最后引用移除及重新上传的摘要一致性通过 |
| BIOS 自动扫描与重扫 | 首次导入 3 项、跳过 166 项、失败 0；重扫导入 0、跳过 169 项、失败 0；原始 BIOS 摘要一致 |
| 单 Parent、多 Parent 与名称冲突 | runtime 回归覆盖七种 DAT 核心及非 DAT Supermodel；同名 ROM 成员分别留在各自 ZIP 中；实际浏览器样例均为单 Parent，不将单元测试冒充多 Parent 游戏实测 |

游玩与公开保存、新 Run 恢复、恢复后暂停观察及继续输入分别覆盖五种 DAT 核心。FBNeo、FBAlpha、MAME Current 使用共同的街机样例；MAME 2003 / Plus 使用另一份已确认能实际游玩的原包。审核预览与发布后的正常入口分别验证。

最初共同的 `sf2accp2` 样例在两种旧 MAME 下黑屏，虽然帧计数持续增长、能产生状态文件，也不计为游玩通过。原生日志显示 `sf2ca-23.bin` 的 CRC 同为 `36c3ba2f`，但实际 SHA-1 为 `5ec34923b3c183eef5e1f5475eaf7c87fc63d14a`，核心预期 `a3ddc479b725ddb3521757c8efa7aed125004997`；原 Parent 已完整读取。这是保留的样例兼容性问题，不能仅凭这条警告断言黑屏的全部根因，也没有为该 ROM 添加特例。

## 工程与验收范围

runtime lint、类型、1,818 项测试、build、package，以及 Provider input/build/check/release 门禁通过；最终源码 CI quality 通过。Retrom backend-check、独立 PostgreSQL integration-test、Web 完整检查、API 生成一致性和 Provider V1 authority 同步检查通过。发布后正式输入及镜像的回执另行记录。

T01–T47 按本次实际范围追踪；常规回归不替代全平台和规模验收：

| Case | 本轮范围 |
| --- | --- |
| T01–T06 | PFB 持久状态、独立测试库迁移与既有工程回归；无账号、目录或 schema 行为变更 |
| T07–T12 | 正常来源扫描、待审核导入、运行配置及原文件上传；只读来源与原始摘要核对 |
| T13–T15 | 既有删除、收藏和最近回归；没有新增相关业务行为 |
| T16–T19 | 五种 DAT 核心的受影响加载、输入、公开存档、新实例恢复；保留内容身份编码；不扩大到全部核心 |
| T20–T23 | 既有 CAS、授权与运行身份回归；真实登录和普通/审核 Run |
| T24–T26 | 标准 PFB 停启保留状态；未专项清空 Redis 或验证草稿故障恢复 |
| T27 | 未改 UI 布局；正式四视口浏览器门禁单独记录 |
| T28–T30 | 常规负载边界、认证和待审权限回归；未重新做 20,000 游戏/200 用户负载 |
| T31 | 五核心 Parent 引用与退休协调实测，文件清理集成回归 |
| T32–T34 | 同一 PFB 双仓库隔离；Host 只投影资源，规则与挂载在 runtime |
| T35–T41 | 原有表结构、API、配置及 Game CAS 保持；生成检查通过；全平台验收状态不提升 |
| T42–T45 | 原样独立 BIOS、正常扫描与重扫实测；并发安装/中断沿用全量集成回归 |
| T46–T47 | 正常 API 创建测试目录，无 seed；原有 lint、类型与复杂度门禁保持 |

线上存量恢复使用普通 Parent 上传 API，逐款检查 ROM、核心、当前 Parent 及原文件摘要，版本冲突或审计后内容变化时停止该款处理。保持游戏状态和元数据，部署后复验全部审计样例。
