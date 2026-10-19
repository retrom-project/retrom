# Pegasus／EmulationStation 来源示例

Retrom 从服务器目录读取来源元数据和游戏文件，复制到受管存储后形成待审核游戏。先在[游戏目录](platform-instance.md)创建目标目录并选择平台、允许核心与默认核心，再按以下任一格式准备来源。

下文使用服务进程可见的 `/server-data` 路径。Docker 部署应将素材目录只读挂载到该路径，见[部署指南](docker-deployment.md)；PFB 挂载方式见[PFB 开发](pfb-development.md)。页面中的路径是容器内路径，不是浏览器所在电脑的路径。

## Pegasus 最小来源

```text
/server-data/pegasus-nes/
├── metadata.pegasus.txt
└── games/
    └── demo.nes
```

`metadata.pegasus.txt` 使用 UTF-8：

```ini
collection: NES Demo

game: Demo
file: games/demo.nes
players: 1
```

`collection` 建立一个来源集合，后面的 `game` 属于该集合；一份元数据可以声明多个集合。`file` 相对这份元数据所在目录，Retrom 不执行 Pegasus 的 `launch`、`command` 或工作目录规则；核心由目标目录和运行配置选择。

可选资料和媒体示例：

```ini
collection: NES Demo

game: Demo
file: games/demo.nes
developer: Example Studio
publisher: Example Studio
players: 1-2
description: Example description
assets.box_front: media/demo.png
assets.video: media/demo.webm
```

使用媒体字段时应同时提供对应文件；不需要时删去字段即可。示例中的名称是占位资料，`demo.nes` 必须替换为有权使用且实际可运行的游戏。开发验证可使用仓库自有 MIT [NES fixture](../testdata/public-roms/nes-smoke/README.md)，复制并按示例命名，不需要商业素材。

## EmulationStation 最小来源

```text
/server-data/es-nes/
├── gamelist.xml
└── games/
    └── demo.nes
```

`gamelist.xml`：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<gameList>
  <game>
    <path>./games/demo.nes</path>
    <name>Demo</name>
    <players>1</players>
  </game>
</gameList>
```

`path` 同样相对 `gamelist.xml` 所在目录。可在 `game` 中添加 `desc`、`developer`、`publisher`、`genre`、`image` 或 `video` 等资料字段；例如 `<image>./media/demo.png</image>`。Retrom 不把来源的 emulator、core 或 command 当作运行配置，也不导入原站的收藏、游玩次数或时长作为当前用户数据。

## 扫描与发布

1. 打开“游戏入库 → 来源扫描”，点击“本地扫描”。
2. 进入 `/server-data/pegasus-nes` 或 `/server-data/es-nes`，明确选择对应的文件组织形式，再读取来源集合。
3. 将需要导入的集合映射到已存在的目标目录，可同时选择共享标签；未映射的集合不扫描。
4. 开始扫描后进入待审核页，查看该扫描的进度。审核队列共享待审 Game，不按扫描建立永久批次。
5. 打开待审核详情，保存资料及入口配置，补齐实际缺失的 BIOS、Parent 或 CHD。运行条件满足后可发布；试玩可选，CRC 差异需按界面确认风险。

来源集合检查只寻找所选目录及其**直接子目录**中的 `metadata.pegasus.txt` 或 `gamelist.xml`，不递归搜索任意深度。例如选择 `/server-data` 可发现上述两个直接子目录中与所选格式匹配的元数据；更深层的集合应进入对应目录后读取。游戏内容、媒体及配套文件的发现范围另由元数据和 runtime 规则决定。

## 路径与内容边界

- 元数据中的内容和媒体使用安全相对路径，不能用绝对路径、`..` 或越出所选来源根的符号链接引用素材。页面选择的服务器目录本身则必须是绝对路径。
- 元数据描述游戏资料，不等于核心支持承诺。项目入口、保留原 ZIP 的核心、BIOS、Parent 与 CHD 前提应查配套 runtime 的 [Target 指南](https://github.com/retrom-project/retrom-runtime/blob/master/docs/targets/README.md)，平台／核心映射见[支持清单](https://github.com/retrom-project/retrom-runtime/blob/master/docs/supported-platforms-and-cores.md)。
- ROM 的 `genre` 资料不会自动变成共享标签；导入标签由管理员映射时选择。私人收藏与最近游玩由当前用户在 Retrom 中产生。
- 缺少必需 BIOS 或配套文件的游戏仍可进入待审核；批准前按当前核心检查必需条件。完成复制后移走来源不影响受管内容，替换游戏内容仍需使用明确的新来源路径。

详细生命周期、数量／大小限制和故障行为见[导入与审核](import-and-review.md)，运行依赖操作见[BIOS 与街机依赖](bios-and-arcade.md)。
