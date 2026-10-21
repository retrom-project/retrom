# 服务器来源导入与审核

首次准备来源可按[Pegasus／EmulationStation 示例](import-source-examples.md)组织文件，再在“来源扫描”中选择格式、读取集合并映射目标目录。游戏目录和允许核心的配置见[游戏目录](platform-instance.md)；核心内容前提由 runtime 的 [Target 指南](https://github.com/retrom-project/retrom-runtime/blob/master/docs/targets/README.md)维护。

管理员从 `/` 浏览服务进程可访问的目录，以绝对 `path` 选择来源，inspect Pegasus metadata.pegasus.txt或EmulationStation gamelist.xml，再将collection映射到现存目录和≤20个活动共享Tag。目录列表返回名称和完整路径，允许普通目录符号链接，权限和挂载范围由实际操作系统决定；容器只能浏览已挂载的内容。来源只读，扫描打开所选目录的os.Root，元数据引用仍使用安全相对路径，递归和文件大小保留扫描算法边界。Players保留有界字符串（例如1-4），未知人数/年份返回null。

扫描只处理映射collection。单候选在事务外复制受管内容与媒体，支持ZIP和7z实际解包，校验成员安全路径、总数和大小。核心声明要求保留外归档语义时由runtime统一配置决定。所有候选调用runtime normalize-content/configure/identity；Tyrano封装提取、真实RPG数据库、DOS入口和ScummVM识别仍归runtime。宿主仅复制归一化输出及消费文件事实。

ZIP成员名优先遵循UTF-8标志；未标记时，先采用版本及原始文件名CRC32校验通过的Unicode Path扩展（0x7075），再尝试无标记UTF-8和GB18030。旧编码字节即使恰好也是合法UTF-8，也不能覆盖明确的Unicode路径。解码后的路径仍须通过安全路径、重复成员与大小限制，损坏的Unicode内容拒绝导入。

单游戏最多接收50,000个内容文件，ZIP／7z解包、项目目录复制、配套内容发现与runtime归一化输出使用同一上限；超过上限拒绝导入。单次来源目录读取最多枚举50,000项，路径、文件类型和总字节数校验继续生效。

运行上下文同时保存各文件描述和项目索引，临时Redis哈希最多100,000个字段、合计64MiB，以容纳大型项目；超出容量仍拒绝创建Run。普通临时字符串值继续使用8MiB上限。

准备完成后创建同一pending_review Game、文件、媒体和Tag关系，并与processed/imported计数同一个短事务提交。重复目录+contentHash跳过（不承诺并行强去重）；失败计数独立提交。COMMIT结果不明时按本次分配Game/BIOS ID与进度核对，无法确认则interrupted，不盲目重建或把已提交项计failed。进度满足processed=imported+skipped+failed。

pending_review是共享管理员审核队列，来自所有来源，不按扫描结果反查或划分批次。批准前在事务外使用当前配置和所选允许核心重新检查必需内容，版本CAS提交时仍只改变同一Game状态，文件不再复制；拒绝标记deleted交给维护worker。试玩使用同一公共运行接口、管理员purpose=review，不建立持久普通存档；临时checkpoint由浏览器public LOCAL restore消费。

审核详情的“运行条件检查”保留runtime返回的不支持媒体、未知ROM集、缺少ROM/Parent/启动文件等明确错误码及操作提示，不将这些问题显示为缺BIOS。未知内部异常使用REVIEW_READINESS_UNAVAILABLE，不向页面暴露原始异常或服务器路径。检查失败时biosSatisfied为null，快速审批跳过；CRC不匹配保持提示，由管理员手动决定是否批准，试玩不作强制门禁。

取消只停止后续候选，已提交Game/BIOS保留；重启将运行中进度标记interrupted，不自动续跑。两个扫描执行槽有界。scan_progress只提供短期提示：维护worker每分钟最多删除200条超过24小时的终态，保留running，不读取/反查或删除Game与BIOS。

发布后资料/媒体直接CAS编辑；ROM按所选绝对文件或项目目录路径复制替换，做文件安全、配置结构/引用与统一内容身份计算，不调用完整运行准备，不把缺BIOS/Parent当替换门禁。替换保留published，旧文件退休并保留宽限，下一次启动才判断实际可运行性。

BIOS扫描与游戏扫描在“来源扫描”页并列，使用同一绝对目录选择和进度列表；运行依赖页只管理已安装BIOS。扫描按选定platform/core范围复用runtime识别，不修改来源文件。

ZIP BIOS识别使用当前核心DAT声明的成员事实；Host从catalog读取后，向识别工具完整传递成员名称、大小、CRC、SHA-1及必需标记，不在Host重复实现DAT匹配。同一缺失requirementKey的候选按实际成员路径、大小和内容去重，再按DAT匹配成员数、ZIP文件成员总数、稳定来源路径依次选择并安装原始归档。DAT提供SHA-1时按SHA-1匹配，否则使用CRC。同名但零匹配的候选可以安装，运行依赖页明确显示成员不完整；已安装记录不被扫描覆盖。

MAME Current 按当前构建DAT的disk记录发现必需、可选及继承的CHD。Pegasus和EmulationStation扫描仅在各元文件所在目录及子目录搜索，按CHD头部内容SHA-1识别，来源文件名可以不同；Host消费runtime给出的通用复制计划。缺少必需CHD仍进入待审核，由运行配套文件区列出缺项并接收匹配CHD。支持v3/v4/v5完整镜像，依赖外部父CHD的差分输入需换用完整镜像；头部检查不代替全盘数据审计。

Flycast GD-ROM 本地扫描会按已安装核心的配套名录，受控复制入口目录内的 CHD 与缺失 Parent，保持来源和归档字节原样。未找到配套文件时仍接收至待审核，由审核页列出缺失文件；可在统一资源区上传名录声明的 ZIP／CHD，再检查或选择可选试玩。ROM、Parent、BIOS CRC 差异不直接拒绝人工审批，快速审核继续跳过存在差异的游戏。

管理员可用当前版本将published退回pending_review，保留同一Game及全部内容／私有关系；普通列表、详情、新建play Run随状态隐藏，管理员可继续review试玩。DAT必需成员按所选游戏条目、Parent合并关系和独立BIOS归属检查，忽略optional及NO_DUMP，BAD_DUMP且有校验事实的必需成员仍需存在。游戏包和所选Parent按名称＋大小或重命名后的大小＋CRC识别；CRC差异保留人工决定。缺失／大小不符返回missingRomMembers，人工与快速审批均阻止，服务端不依赖前端判定；未经试运行也可以批准完整内容。正常启动没有新增此完整性检查。
