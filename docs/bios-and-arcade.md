# BIOS 与街机依赖

需求来自runtime实际Target声明。公共列表按requirementKey聚合消费者核心/平台，数据库批量读取当前安装；共享需求只显示和安装一次。installed表示有当前文件，不代替本次运行可用性检查。

自动扫描先按需求 key 去重并设置要求总数。已有 active 安装始终跳过。普通文件按已声明摘要识别；街机 ZIP 按文件名或完整 DAT 成员内容识别候选，并实际解压校验成员大小和内容摘要。成员路径、大小和内容完全一致的 ZIP 去重，不受压缩参数、成员顺序或时间戳影响。不同 ZIP 优先选择满足 DAT 成员最多的候选，再按 ZIP 内文件成员总数排序，仍相同则按稳定来源路径选择。成员改名仍可凭大小和 SHA1 匹配；DAT 没有 SHA1 时才回退 CRC32。同名候选即使全部零匹配也选择排名第一项，但安装不代表验证通过。无候选跳过，无关源文件不计进度。每独立要求拥有bios/<bios-file-id>/文件，安装和成功进度同事务；唯一冲突计skip，不退休或覆盖并发手动安装。

BIOS 来源扫描最多枚举 100000 个文件，递归子目录的文件合并计入总数；超过上限以 SOURCE_INVALID 结束扫描。

手动上传/替换只检查requirementKey存在、文件安全/非空/大小，记录实际SHA，不以已知BIOS摘要校验门禁，也不触发游戏重审核或全库验证。旧安装deleted，current只有一条。删除当前安装同样由文件生命周期清理。

街机DAT、实际ZIP成员、Parent闭包和BIOS需求由runtime准备解析，Go不截取第一个Parent或猜核心规则。所选DAT父链通过EXTERNAL_FILE_SET逐文件交付原始ZIP，保留每个文件的大小与SHA，仅规范化虚拟挂载名称；不合并成员、不重打包、不向父包注入BIOS。DAT要求的BIOS始终独立交付。缺依赖在正式启动/试玩准确失败，列表与普通资料编辑不装配完整运行资源。

安装列表通过 runtime 展示 DAT 匹配数及 valid / incomplete / invalid 状态；缺少必需成员或内容不匹配的已安装 BIOS 仍保留，但计入“待处理”。运行准备和审核就绪检查会拒绝不满足必需成员的 BIOS。手动替换仍可上传文件，实际可用性单独显示。自动重扫不修复、替换或删除任何已有安装。

RPG Maker XP/VX/VX Ace（mkxp）的官方 RTP 作为运行依赖管理：`Standard.zip`、`RPGVX.zip`、`RPGVXAce.zip` 各对应一个引擎 Target。只有游戏的 Game.ini 声明了该引擎官方 RTP 时，runtime 才在配置中记录并把它列为必需；未声明的游戏、MV/MZ 与 EasyRPG 不受影响。已入库游戏需重新导入或替换内容后才会记录 RTP。RTP 以普通 ZIP 原样上传保存，不展开、不重打包；ZIP 根目录必须直接是该 RTP 的目录（不能整个压进 `Standard/` 文件夹）。三种 RTP 没有固定摘要，runtime 只读取 ZIP 目录、按各自独有的标志目录／文件识别与校验：XP 为 `Graphics/Windowskins/`、`Graphics/Autotiles/`，VX 为 `Graphics/System/TileA1.png`、`TileB.png`，VX Ace 为 `Graphics/Titles1/`、`Graphics/Battlebacks1/`，并都要求 `Audio/`。BIOS 扫描因此不看文件名，按内容认出 RTP；含 `Game.ini` 或 `Data/` 的完整游戏包不会被认作 RTP；同一 RTP 有多个不同包时按包内成员更多者选取。把某一引擎的 RTP 装到另一引擎的需求下，列表显示 incomplete/invalid，审核与启动前同样拒绝。启动时按 runtime 声明的资源角色以 Range 资源交付，核心按 Game.ini 中的名字挂载。

MAME Current 的 CHD 要求来自对应 DAT 的 disk 记录，包括可选磁盘和克隆集继承关系。metadata.pegasus.txt / gamelist.xml 服务端导入只在该元文件目录及其子目录递归发现 CHD；不搜索父目录或其他元文件根。候选通过有界 CHD 头部读取，以 DAT 内容 SHA1 匹配，不依赖来源名称。支持 v3/v4/v5 完整镜像；依赖父镜像的差分 CHD 明确报错并要求提供完整 CHD。这与游戏 ROM Parent ZIP 的继承是不同的要求。

导入保留主 ZIP，并将匹配 CHD 复制为受管的 `<rom-set>/<disk-name>.chd`，计算整文件 SHA256。不会修改来源、解压整盘或改变数据库结构。缺少必需 CHD 仍可形成待审核游戏；审核页显示缺项并支持追加 CHD，上传时检查磁盘身份，完整后才能运行。头部验证不等于逐块完整性审计；损坏压缩数据仍可能在核心读取时失败。

CHD 作为独立 media 资源交付。MAME 在 Worker 内通过同步文件桥按需读取有界 Range，并按稳定内容身份复用浏览器持久缓存；用户可显式选择预加载。基础 CHD 保持只读，核心生成的差异磁盘和 NVRAM 与原生即时状态一起保存，在新运行实例启动前恢复。没有 CHD 的 MAME 游戏沿用现有加载路径。
