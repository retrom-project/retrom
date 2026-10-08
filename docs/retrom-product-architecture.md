# 产品与模块边界

Retrom 为个人和可信朋友提供共享游戏库。User 是唯一账号和私人资源主体。管理员维护目录、共享标签、游戏与运行依赖；所有账号拥有各自的存档、最近游玩、收藏和收藏夹，管理员身份不会扩大对其他用户私人资源的读取权。

| 产品模块 | 拥有职责 |
| --- | --- |
| 游戏库（含收藏） | Game、文件、媒体、生命周期、私人收藏与收藏夹 |
| 存档 | 不透明payload、截图、名称、槽位、冻结extinfo、覆盖CAS |
| 最近游玩 | User/Game最后成功游玩时间 |
| 运行依赖 | requirementKey当前安装、直接替换、服务器补齐 |
| 目录 | 人工创建的平台目录、核心集合和默认核心 |
| 审核（含游戏导入） | Pegasus/EmulationStation扫描、同一待审Game编辑与首次批准 |
| 标签 | 共享标签及Game关系 |
| 用户 | 初始化、账号、密码、会话、邀请、重置及临时限流 |

Home是读取投影；扫描进度、运行和文件维护是内部能力。它们没有通用Job/Input/Event/Lease持久模型。Game只记录来源类型，不保存扫描或批次ID；扫描进度不保存来源映射、结果列表或恢复游标。BIOS与游戏扫描分别执行，不借用彼此的审核逻辑。两类入口与进度统一位于来源扫描页；游戏扫描启动后待审核页可按scanId展示该任务的临时进度，不按它筛选Game或建立批次关系。运行依赖页仅管理当前安装。管理员以服务进程可见的绝对路径浏览和选择来源，不使用配置来源根或rootId；来源只读，复制接收后的受管所有权不变。

HTTP只处理认证、请求验证与响应；应用入口在cmd/retrom组装服务。internal/model保存领域字段和纯验证，internal/service拥有用例，internal/persistence拥有SQL及短事务，internal/storage拥有受管文件，internal/temporary拥有窄Redis能力，internal/runtimeclient调用统一runtime工具。业务和基础层不反向依赖HTTP；runtime/存档不依赖扫描服务。

retrom-runtime负责内容归一化与配置解释、平台/核心/Target声明、BIOS识别、街机Parent、指纹、浏览器执行、内容桥、checkpoint和恢复判定。Go只传受控文件事实和宿主locator，消费通用资源计划，不维护引擎分支或第二份静态核心/BIOS/DAT事实。

生产范围不包含Profile、Hasheous刮削、浏览器批量ROM上传、多盘、独立运行ticket、游玩时长或通用工作流历史。普通公开资料、媒体和运行只读取published Game；待审读取与试玩只对管理员开放。修改已发布ROM/BIOS不经过审核或运行可用性门禁。

街机父包补齐属于同一Game的文件维护，不是新的浏览器批量导入流程。管理员在待审或管理详情按所选核心读取runtime权威缺项、上传父包；Go持有GameCAS及受管game_file事务，runtime解释DAT和parentFiles合入。新文件准备在事务外，仅被同logicalKey替换的旧文件按24小时宽限退休，其他核心引用及其他文件保留；提交结果不明按本次分配文件ID核对；没有新表或第二份DAT事实。父包上传与普通资料保存都不以完整Prepare作门禁，实际运行仍单独验证。
