# 服务器来源导入与审核

管理员从 `/` 浏览服务进程可访问的目录，以绝对 `path` 选择来源，inspect Pegasus metadata.pegasus.txt或EmulationStation gamelist.xml，再将collection映射到现存目录和≤20个活动共享Tag。目录列表返回名称和完整路径，允许普通目录符号链接，权限和挂载范围由实际操作系统决定；容器只能浏览已挂载的内容。来源只读，扫描打开所选目录的os.Root，元数据引用仍使用安全相对路径，递归和文件大小保留扫描算法边界。Players保留有界字符串（例如1-4），未知人数/年份返回null。

扫描只处理映射collection。单候选在事务外复制受管内容与媒体，支持ZIP和7z实际解包，校验成员安全路径、总数和大小。核心声明要求保留外归档语义时由runtime统一配置决定。所有候选调用runtime normalize-content/configure/identity；Tyrano封装提取、真实RPG数据库、DOS入口和ScummVM识别仍归runtime。宿主仅复制归一化输出及消费文件事实。

准备完成后创建同一pending_review Game、文件、媒体和Tag关系，并与processed/imported计数同一个短事务提交。重复目录+contentHash跳过（不承诺并行强去重）；失败计数独立提交。COMMIT结果不明时按本次分配Game/BIOS ID与进度核对，无法确认则interrupted，不盲目重建或把已提交项计failed。进度满足processed=imported+skipped+failed。

pending_review是共享管理员审核队列，来自所有来源，不按扫描结果反查或划分批次。批准只改变同一Game状态，文件不再复制；拒绝标记deleted交给维护worker。试玩使用同一公共运行接口、管理员purpose=review，不建立持久普通存档；临时checkpoint由浏览器public LOCAL restore消费。

取消只停止后续候选，已提交Game/BIOS保留；重启将运行中进度标记interrupted，不自动续跑。两个扫描执行槽有界。scan_progress只提供短期提示：维护worker每分钟最多删除200条超过24小时的终态，保留running，不读取/反查或删除Game与BIOS。

发布后资料/媒体直接CAS编辑；ROM按所选绝对文件或项目目录路径复制替换，做文件安全、配置结构/引用与统一内容身份计算，不调用完整运行准备，不把缺BIOS/Parent当替换门禁。替换保留published，旧文件退休并保留宽限，下一次启动才判断实际可运行性。

BIOS扫描与游戏扫描在“来源扫描”页并列，使用同一绝对目录选择和进度列表；运行依赖页只管理已安装BIOS。扫描按选定platform/core范围复用runtime识别，不修改来源文件。
