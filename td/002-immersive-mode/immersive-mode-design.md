# Retrom 沉浸模式设计

| 属性 | 内容 |
| --- | --- |
| 临时方案编号 | `002` |
| 状态 | 待实施；产品与技术决策已锁定 |
| 日期 | 2026-08-24 |
| 产品名称 | 沉浸模式（客厅模式） |
| 核心路径 | 首页触发 → 选择平台 → 选择游戏 → 单机游玩 → 返回游戏列表 |
| 配套设计稿 | [`immersive-mode-ui.html`](./immersive-mode-ui.html) |

> 本目录是实施前临时方案，不是生产事实源。实施完成时，须把稳定内容按职责合并到 `docs/`、
> `docs/design/` 和正式验收规范，重新生成统一设计稿，通过全部门禁后删除整个
> `td/002-immersive-mode/`。源码、正式文档、测试和构建脚本不得读取本目录。

## 1. 一句话结论

沉浸模式是一套独立于 Retrom 现有 PC、平板和移动界面的电视交互面。它不把手柄映射成鼠标，也不让
普通页面获得全局手柄焦点；普通首页只负责发现手柄并征求进入意愿。确认后，用户进入一个只包含
“选平台、选游戏、玩游戏”的全视口界面。

本方案不包含存档、联机、收藏、筛选、搜索、账户、管理、Core 选择、模拟器高级设置或全站手柄导航。
此前 002 中的这些能力全部作废，不进入新实现。

## 2. 已锁定产品决策

以下决策实施时不得再次自由选择：

1. 沉浸模式是独立路由、独立 App Shell 和独立视觉系统；不复用普通 PC 侧栏、移动 App Bar、底部导航、
   普通游戏卡或普通响应式页面。
2. 普通页面中只有首页 `/` 读取 Gamepad API；其他普通用户页和全部管理页不响应手柄。
3. 首页读取到标准手柄的任意按钮按下沿时，只打开“进入沉浸模式？”确认框，不直接进入。
4. 触发确认框的那次按键必须被消费；手柄全部释放并连续中立 `120 ms` 后，确认框才接受输入。
5. 确认框默认选中“取消”。左右切换按钮，A 确认当前按钮，B 无条件取消。
6. 确认后进入 `/immersive`。这是占满 `100vw × 100dvh` 的 Web 界面，但不声称进入浏览器 Fullscreen。
7. 平台视图只显示至少有一款可见、已发布游戏的平台；按平台全名、平台 ID 稳定排序。
8. 平台卡固定显示平台名、当前游戏数和当前 Profile 最近一次在该平台开始游玩的时间。
9. 平台视图左右循环切换；A 进入平台游戏列表；B 打开退出沉浸模式确认，而不是直接离站。
10. 游戏列表左侧是纵向游戏名，右侧上方同时容纳封面和可选视频，下方展示简介。
11. 游戏按标题、Game ID 稳定排序；上下移动，不在首尾环绕；接近页尾时有界预取下一页。
12. A 以默认 Core、无存档的普通单机模式启动选中游戏；B 返回平台视图并恢复原平台。
13. 沉浸模式不提供“继续存档”、创建存档、存档管理、联机、收藏、搜索、筛选、目录选择或 Core 选择。
14. 游戏运行中只有活动导航手柄的“双击 Select+Start 组合”属于 Retrom；其他输入交给游戏。
15. 第一次 Select+Start 组合只进入组合等待窗并被抑制；释放后在规定时间内第二次组合才暂停并打开菜单。
16. Player 菜单只有“取消”和“退出游戏”两个按钮。“取消”表示关闭菜单、取消暂停并继续当前游戏。
    最新范围已经明确排除存档，因此不保留第三个“存档”按钮。
17. 菜单默认选中“取消”。左右切换，A 执行，B 始终等于取消。
18. 退出游戏完成既有 PlaySession finish/revoke 后返回原平台游戏列表，并恢复原游戏选择。
19. 普通 PC/移动 Player、键盘、鼠标、触摸和联机行为保持原样；沉浸模式是显式进入的独立体验。
20. 一期只保证 Chrome 报告 `mapping === "standard"` 的手柄，不根据设备名称猜布局，不保存设备 ID。

## 3. 调研依据与取舍

本方案参考的是 EmulationStation 的信息架构，不复制其主题、图片或实现：

- [ES-DE 用户指南](https://gitlab.com/es-de/emulationstation-de/-/blob/master/USERGUIDE.md)把 System View
  定义为默认起点，用于浏览游戏系统并进入各自 Gamelist；Gamelist View 用于浏览和启动游戏。
- 同一指南的 General navigation 明确 A 为选择/进入/启动、B 为返回，并在底部 Help System 中显示当前
  上下文可用操作。
- [ES-DE 主题规范](https://gitlab.com/es-de/emulationstation-de/-/blob/master/THEMES.md)把现代主题模型收敛为
  `system` 与 `gamelist` 两个主要 view，并允许 carousel、textlist、image、video 和文字信息自由组合。
- 主题规范建议同一视图只保留一个活动视频，避免同时解码多个视频带来的资源和声音问题。
- [原版 EmulationStation](https://github.com/Aloshi/EmulationStation)的核心定位也是无需键盘的游戏前端；
  `gamelist.xml` 元数据为名称、图片和描述等详细视图提供数据。

Retrom 采用以下取舍：

- 借鉴 System View → Gamelist View → Launch 的三级模型；
- 借鉴 A 选择、B 返回与底部上下文帮助；
- 借鉴文字列表配合媒体详情的布局；
- 不引入主题下载、系统菜单、收藏、筛选、元数据编辑或媒体查看器；
- 不复制 ES-DE 的 Linear/Modern/Slate 主题资产；
- 使用 Retrom 的紫色、青色、暖白文字和现代卡片边界，形成原创的电视视觉。

## 4. 范围与非目标

### 4.1 范围内

- 首页标准手柄发现和进入确认；
- 独立平台选择页；
- 独立平台游戏列表；
- 当前选中游戏的 COVER、VIDEO 和 description；
- 默认单机 Launch、加载、运行和返回；
- 游戏中双击 Select+Start 的暂停菜单；
- 活动手柄断开、重新认领、数据变化和运行失败的可恢复状态；
- 手柄、键盘和鼠标驱动同一语义动作的测试边界；
- 1280×720、1920×1080、2560×1440 CSS viewport 和 4K 150% 视觉验证。

### 4.2 明确不做

- 普通首页之外的 PC/移动页面手柄导航；
- 存档创建、存档恢复或存档列表；
- 联机大厅、房间或联机 Player；
- 收藏、标签、最近游玩列表、搜索、筛选和排序设置；
- 游戏详情、目录或 Core 配置；
- 管理后台、认证和账户操作；
- 虚拟鼠标、屏幕键盘、手柄重映射、震动和品牌专属布局；
- 自动进入浏览器 Fullscreen、方向锁、PWA 或 kiosk 启动参数；
- 手机竖屏和窄横屏沉浸 UI；
- 新的数据库持久化偏好、主题系统或平台图片资产。

Select 打开网页菜单后再以 A 选择仍不属于浏览器可信用户激活，因此也不在菜单中提供无法兑现的“全屏游玩”
操作；纯手柄隐藏浏览器界面由部署层的 kiosk/start-fullscreen 保证。

## 5. 路由和界面边界

| 状态 | 路由 | App Shell |
| --- | --- | --- |
| 普通首页与进入确认 | `/` | 现有普通 App Shell；确认框是现有首页上的应用内 Dialog |
| 平台选择 | `/immersive?platformId=<id>` | 新 `ImmersiveShell`，不渲染普通侧栏/顶栏/底栏 |
| 平台游戏列表 | `/immersive/platforms/:platformId?gameId=<uuid>` | 同一 `ImmersiveShell` |
| 单机 Player | `/play/:launchId?experience=immersive` | 现有 Player stage + 新沉浸控制层；不渲染普通 App Shell |

规则：

- `platformId` 与 `gameId` 是非秘密焦点恢复提示。无效值被忽略，并选择第一个有效项。
- `/immersive` 的直接访问要求已登录；没有标准手柄时显示“等待手柄”状态，并提供可点击的“返回首页”。
- 沉浸 Launch 的 `returnTo` 固定为
  `/immersive/platforms/<platformId>?gameId=<gameId>`。
- `experience=immersive` 只选择前端呈现和手柄所有权，不扩大 Launch 权限；删除该 query 不改变内容授权。
- Player 退出使用客户端 history-replace 到 `returnTo`，旧 Launch 不留在后退栈，并保留当前页面内存中的
  活动手柄认领；不得整页重载后把仍连接的手柄误判为需要重新认领。
- 在平台选择页确认退出后使用 `router.replace("/")`；浏览器后退不得重新打开已退出的沉浸页面。

### 5.1 独立不等于第二套产品数据

沉浸模式复用以下现有事实：

- 当前认证 Profile；
- `PUBLISHED` Game 和 enabled PlatformInstance 可见性；
- 当前 MetadataRevision 的 title、description、COVER 和 VIDEO；
- Platform、默认 Core、Variant validation、BIOS/Parent、多盘和 DOS 启动规则；
- 现有 LaunchSession、Player config、内容 capability、PlaySession 与有效时长；
- 现有媒体内容端点及删除后的 404/引用释放语义。

它只新增为电视浏览优化的只读投影和前端状态机，不建立另一套 Game、媒体或启动记录。

### 5.2 独立 UI 的物理边界

“独立”不仅是隐藏普通导航，而是以下可测试的代码与运行时边界：

- `/immersive` 路由放在普通 App Shell 之外，不先渲染再用 CSS 隐藏 PC sidebar、mobile header 或 bottom nav；
- `ImmersiveShell` 不导入普通 Home、GameCard、GameDetail、AppNavigation 或移动端布局组件；
- 沉浸页面拥有独立的电视排版、焦点样式、帮助条、空错状态和断点，不从普通响应式页面派生；
- 可以复用无视觉业务语义的底层设施，例如认证、generated API client、内容 URL、Launch client、错误码和
  Player Core stage；这些复用不得把普通页面 DOM 或交互状态带入沉浸壳；
- 首页进入确认仍属于普通首页，所以可以使用现有 Dialog 基元；确认成功后的所有可见 UI 都由沉浸模块
  独立渲染；
- CSS 使用沉浸模块命名空间或 CSS Module，不向 `body`、普通 App Shell 和 Player 的非沉浸分支泄漏全局规则；
- E2E 必须同时断言沉浸 DOM 中不存在普通导航，并证明退出后普通 PC/移动 DOM、样式和输入恢复原状。

## 6. 完整状态机

```text
STANDARD_HOME
  └─ standard pad 任意按钮按下沿
       → ENTRY_DIALOG_LOCKED
       └─ 全部按钮释放 + 120ms 中立
            → ENTRY_DIALOG
               ├─ B / A@取消 → STANDARD_HOME
               └─ A@确认 → SYSTEM_LOADING

SYSTEM_LOADING
  ├─ 成功且有平台 → SYSTEM_VIEW
  ├─ 成功但无平台 → SYSTEM_EMPTY
  └─ 失败 → SYSTEM_ERROR

SYSTEM_VIEW
  ├─ 左右 → SYSTEM_VIEW（切换平台）
  ├─ A → GAMELIST_LOADING
  └─ B → IMMERSIVE_EXIT_DIALOG

GAMELIST_LOADING
  ├─ 成功且有游戏 → GAMELIST_VIEW
  ├─ 平台已空/停用 → GAMELIST_EMPTY
  └─ 失败 → GAMELIST_ERROR

GAMELIST_VIEW
  ├─ 上下 → GAMELIST_VIEW（切换游戏、媒体随选中项更新）
  ├─ A → LAUNCH_PENDING
  └─ B → SYSTEM_VIEW（恢复原平台）

LAUNCH_PENDING
  ├─ 201 → PLAYER_LOADING → PLAYER_GAMEPLAY
  ├─ 202 → 等待既有 validation → 自动重试 Launch
  └─ 失败 → LAUNCH_ERROR（留在列表）

PLAYER_GAMEPLAY
  ├─ 游戏输入 → Core
  └─ 双击 Select+Start → PLAYER_MENU_OPENING → PLAYER_MENU

PLAYER_MENU
  ├─ B / A@继续 → PLAYER_MENU_CLOSING → PLAYER_GAMEPLAY
  └─ A@退出 → PLAYER_EXITING → GAMELIST_VIEW
```

任一沉浸页面出现认证失效时进入只读阻断层，只提供“返回登录”；不在沉浸壳里复制登录表单。

## 7. 手柄发现、认领和输入模型

### 7.1 支持条件

导航手柄必须同时满足：

- `connected === true`；
- `mapping === "standard"`；
- 至少存在按钮 0、1、8、9、12、13、14、15；
- 按钮和轴值为有限数字；
- 可读取左摇杆 axes 0/1，轴缺失时十字键仍可完整导航。

未知映射按键不打开进入确认；首页显示一次非阻塞提示“当前手柄不是标准布局，沉浸模式无法保证操作”。
不得根据 `Gamepad.id` 猜 Xbox、PlayStation 或厂商布局。

### 7.2 认领

1. 首页可见时监听 `gamepadconnected/disconnected` 并使用 `requestAnimationFrame` 轮询。
2. 第一个标准手柄的任意 `buttons[n]` 从未按下变为 `pressed || value >= 0.5` 时，认领其 index 并打开 Dialog。
3. 触发按键不执行 Dialog 动作；先经过全按钮释放、摇杆绝对值低于 `0.35` 的 `120 ms` 中立门。
4. 认领只保存在当前文档内存，不写 Cookie、localStorage、sessionStorage、数据库或 API。
5. 其他手柄不驱动沉浸 UI，但进入游戏后仍按现有 Core 配置作为游戏输入。
6. 活动手柄断开后停止 UI 与 Core 的该设备输入，暂停单机 Player，并显示重新认领层。下一台标准手柄
   任意按键只认领；中立后再按 A 继续。
7. 不记录、不上传、不展示完整 `Gamepad.id`。诊断只保留匿名 pad ordinal、mapping、button/axis count。

### 7.3 固定映射

| 标准输入 | 平台视图 | 游戏列表 | Dialog/Player 菜单 |
| --- | --- | --- | --- |
| D-pad 左右 / 左摇杆 X | 切平台 | 左向上翻 8 项、右向下翻 8 项 | 切按钮 |
| D-pad 上下 / 左摇杆 Y | 无动作 | 切游戏 | 无动作 |
| A / button 0 | 进入平台 | 启动游戏 | 确认当前按钮 |
| B / button 1 | 退出确认 | 返回平台 | 取消/继续 |
| Select / button 8 | 无动作 | 无动作 | 游戏中只参与保留组合 |
| Start / button 9 | 无动作 | 无动作 | 游戏中只参与保留组合 |
| 其他按钮 | 无动作 | 无动作 | 无动作；游戏中仍交给 Core |

键盘仅作为无障碍和测试后备：方向键对应方向，Enter 对应 A，Escape 对应 B，`M` 模拟已完成的双组合。
鼠标可以点击当前可见按钮或游戏项，但不引入 hover-only 操作。

### 7.4 阈值、重复和中立门

- 数字按钮：`pressed === true || value >= 0.5`。
- 摇杆进入阈值 `0.60`，退出阈值 `0.35`；两轴同时越界时只采用绝对值更大的轴。
- 方向首次立即执行；保持 `350 ms` 后每 `120 ms` 重复一次。
- A、B 和组合只响应上升沿，不重复。
- 路由切换、Dialog 开闭、Player 菜单开闭和手柄重认领后都要求 `120 ms` 中立。唯一例外是从 Player
  客户端返回游戏列表：一次性允许首个上下/左右浏览输入立即生效，但 A/B 仍必须等满 `120 ms`，避免退出
  菜单的确认输入穿透并重新启动；首次中立完成后该方向豁免永久消费。
- 页面隐藏、窗口失焦和组件卸载立即清除 repeat、edge 和 pending chord 状态；恢复后重新中立。

## 8. 首页进入确认

### 8.1 结构和文案

标题固定为“进入沉浸模式？”，说明固定为：

> 使用手柄按平台浏览并启动游戏。沉浸模式采用独立的大屏界面，不包含存档、联机和管理功能。

确认层覆盖整个 viewport，使用沉浸电视背景、顶部品牌/圆形 A/B 提示和屏幕中央确认卡，不复用普通 PC
Dialog 外观。按钮按视觉从左到右为“取消”“进入沉浸模式”。默认聚焦“取消”。底部提示为“A 确认 · B
取消 · 左右选择”，A 为绿色、B 为红色。

### 8.2 行为

- B 在任何选择上都取消；A 执行当前选择。
- 方向和 A 落在同一 Gamepad 采样帧时先应用方向再执行 A；B 与 A 同帧时 B 优先。
- 遮罩、Escape 和普通页面的关闭按钮都等于取消。
- 取消后回到首页原焦点；全部输入中立 `500 ms` 后才重新武装任意按键触发，避免同一按住动作重开。
- 确认后关闭普通 App Shell 的手柄轮询，再导航到 `/immersive`；两套 listener 不得并存。
- Dialog 使用现有应用内 Dialog/focus trap，不使用 `window.confirm`。

## 9. 平台选择视图（System View）

### 9.1 页面结构

```text
┌──────────────────────────────────────────────────────────────────────┐
│ RETROM / 沉浸模式                                      当前时间      │
│                                                                      │
│            前一平台      [ 当前平台大卡 ]      后一平台              │
│                           平台全名                                   │
│                           24 款游戏                                  │
│                           上次游玩：昨天 21:35                        │
│                                                                      │
│               左右选择平台 · A 进入 · B 退出沉浸模式                │
└──────────────────────────────────────────────────────────────────────┘
```

- 当前平台卡占主视觉宽度约 `38%`，相邻平台各露出约 `16%`；主卡保持高亮、高饱和和正向视角，相邻卡降低亮度与饱和度并形成向内透视，明确可横向移动。
- 每次左右切换使用 `320ms` 三卡位定向过渡：旧主卡退到对侧相邻位、新主卡从操作方向进入中心、外侧卡淡入；卡片下方的位置数字与分段轨道同步移动。不得让三张卡使用同一段淡入代替卡位切换。
- 平台没有上传 Logo 的产品实体，本期使用全名、2–4 字稳定缩写和由平台 ID 决定的配色，不新增位图 seed。
- 卡片显示 `gameCount` 和可空 `lastPlayedAtMs`；为空时写“尚未游玩”，不显示 1970 或空占位。
- 底部上下文帮助条常驻，不显示当前不可用动作。
- 切换使用 `220 ms` 定向位移动画；`prefers-reduced-motion` 下立即更新。

### 9.2 排序与焦点

- 服务端按 `platform.name COLLATE NOCASE, platform.id` 排序。
- 左右循环，末项向右回首项，首项向左回末项。
- 从游戏列表 B 返回时恢复同一平台；直接进入则优先合法 query `platformId`，否则首项。
- 平台更新使当前项消失时选择同一索引位置的下一项，再无项则进入空状态。

### 9.3 空、错和退出

- 没有平台：显示“还没有可游玩的游戏”，A 与 B 都返回普通首页。
- 读取失败：显示“无法读取游戏平台”，默认聚焦“重试”，B 返回首页。
- B 打开“退出沉浸模式？”Dialog，默认“继续沉浸模式”，另一项“返回普通首页”；B 等于继续。

## 10. 平台游戏列表（Gamelist View）

### 10.1 固定布局

在支持视口中页面始终分为左右两部分：

- 左侧 `36%`：平台名、游戏数和纵向标题列表；
- 右侧 `64%`：上方媒体舞台约占右侧高度 `62%`，下方简介约占 `38%`；
- 底部独立帮助条不覆盖正文。

左侧显示当前项上下各最多 4 项。当前项使用高对比背景、左侧 4px 紫色标记和序号；非当前项逐级降低
透明度，但正文仍满足对比度。重复标题时在标题下显示游戏目录名，以便区分。

### 10.2 媒体舞台

- 有封面和视频：左侧显示 `5:7` COVER，右侧显示 `16:9` VIDEO，两块容器高度严格对齐。
- 只有封面：封面在舞台中居中，最大高度不超过舞台 `92%`。
- 只有视频：视频使用 `16:9`，旁边显示由标题生成的 5:7 Retrom 占位封面。
- 两者都无：显示标题、平台和简洁的 Retrom 占位图形。
- 真实媒体一律 `object-fit: cover` 填满各自容器，不显示媒体自带黑边之外的布局黑边。
- 同时只 mount 当前选中游戏的一个 `<video>`；切换游戏立即卸载旧视频并停止其网络/解码。

### 10.3 视频时序

1. 选中项变化后立即展示封面/placeholder。
2. 保持同一选中项 `700 ms` 且页面可见、媒体舞台在 viewport 内时，才设置 VIDEO `src`。
3. VIDEO 固定 `muted + playsInline + loop + preload="metadata"`；成功触发 `playing` 后再淡入。
4. `prefers-reduced-motion: reduce` 时不自动播放，只显示带播放图标的 poster；A 仍用于启动游戏，不复用为视频播放。
5. `error/stalled` 超过 `3 s`、页面隐藏或 selection 改变时回退封面，不显示破损播放器。
6. VIDEO URL 只来自同源内容端点，不接受来源 URL、Blob ID 或外站地址。

### 10.4 简介

- 展示当前 MetadataRevision 的完整 `description`，空值显示“暂无游戏简介”。
- 描述区超出可用高度时使用可聚焦的纵向滚动容器，不截断、不渐隐；手柄上下仍用于选择游戏，正文可由
  键盘、滚轮或触控滚动，换项后回到顶部。
- `aria-label` 保留完整文本，视觉滚动不改 API 数据。
- 简介上方可显示年份、开发商和类型，但这些是次要信息，不抢占 description。

### 10.5 列表、分页和恢复

- 初始读取 50 项；当前焦点进入已加载末尾前 10 项时预取下一页。
- 任一时刻只有一个续页请求；失败时在列表尾增加“加载失败，A 重试”，焦点不丢失。
- 上下移动不环绕；左右每次快速移动 8 项并在首尾夹紧。首项继续向上或末项且无下一页继续向下时保持不动
  并给轻微边界反馈。
- 切换游戏同步更新 URL `gameId`，使用 `history.replaceState`，不触发整页 SSR。
- 从 Player 返回恢复同一 `gameId` 和列表滚动位置；游戏已删除/不可见时选择相邻项。返回页出现后的首个
  方向输入不得被中立门吞掉，A/B 仍保持防误触门禁；DOM 焦点归还当前游戏 option，已有浏览器全屏保持
  不变，不能因 Player iframe 卸载而让文档永久失焦并停在手柄检查态。
- B 返回 `/immersive?platformId=<id>`，不进入普通游戏详情或游戏库。

### 10.6 启动

- A 发送现有 `POST /api/v1/launches`：`coreId=null`、`saveStateId=null`、`dosEntry=null`、沉浸
  `returnTo` 和当前 `clientCapabilities`。
- 不读取浏览器中普通详情页保存的 Core/DOS 偏好；服务端使用游戏目录默认 Core 和审核默认 DOS entry，
  无默认 entry 时沿用现有 Core 程序菜单能力。
- 201 后导航到服务端 play URL，并追加 `experience=immersive`。
- 202 在全视口加载层等待既有 Variant validation，成功后用新幂等键重试，不显示第二个 Start。
- Launch blocker 留在列表并显示可关闭错误层；B 关闭，A 在 retryable 状态重试。BIOS/配置修复仍需普通界面，
  沉浸模式不复制管理入口。

## 11. 沉浸 Player 与暂停菜单

### 11.1 Player 呈现

- 复用现有 `/play/:launchId`、Player stage、iframe、Core 配置、内容 capability 和 canvas 比例算法。
- `experience=immersive` 时隐藏普通顶部工具栏、揭示柄、“更多”、存档、调试、模拟器设置和鼠标提示。
- 加载、方向阻断和致命错误仍复用现有安全状态，但文案和返回动作指向沉浸游戏列表。
- 游戏运行时方向键、A、B、Select、Start 等全部是游戏输入，只有第 11.2 节组合由 host 截留。

### 11.2 “双击 Select+Start”精确定义

一个组合 chord 满足：

1. 活动导航手柄的 button 8 与 button 9 在 `100 ms` 内先后按下；
2. 第二个按下时第一个仍保持按下；
3. 两键随后都释放，完成本次 chord；
4. 两次 chord 之间必须有至少 `60 ms` 的完全释放；
5. 第二次 chord 的首键须在第一次 chord 完全释放后 `650 ms` 内按下。

行为：

- 单独 Select 或 Start 在 `100 ms` 判定窗结束后按普通游戏输入输出，快速 tap 生成一次等价脉冲并释放。
- 第一次完整 Select+Start chord 被视为保留前缀并全部抑制；超过 `650 ms` 没有第二次时无动作，也不补发
  该 chord 给 Core。
- 第二次 chord 成立时立即抑制两键、释放全部 Core 输入、暂停单机 Core 并打开菜单。
- 只处理活动导航手柄；其他本地手柄的 Select/Start 不触发菜单，仍由 Core 接收。
- 页面隐藏、失焦、手柄断开、Player teardown 和退出都会清空 pending chord 并向 Core 发送 release。

这套手势牺牲“单次同时按 Select+Start”作为游戏输入，换取可判定、不会误开菜单的双击入口；单独
Select 和单独 Start 保持可用。实现和 UI 必须如实说明这一保留组合。

### 11.3 菜单打开

打开顺序：

1. 识别第二次 chord 并立即过滤保留键；
2. 过滤快照对全部本地手柄输出全零按钮/轴，显式释放上一帧输入；
3. 调用现有 adapter pause，确认成功后记录 `pauseOwner=IMMERSIVE_MENU`；
4. 背景游戏画面降亮至 32%，显示居中菜单；
5. 默认选择“取消”；
6. 等全部输入连续中立 `120 ms` 后才接受菜单 A/B。

菜单只有：

- “取消”：关闭菜单、取消本菜单拥有的暂停并继续游戏；
- “退出游戏”：finish/revoke 当前 Launch，清理 iframe、过滤器和定时器，返回游戏列表。

B 无论当前选择在哪一项都等于“取消”。左右循环选择两个按钮。A 执行当前按钮。菜单不含存档、
联机、音量、全屏、换盘、调试或高级设置。

关闭继续的顺序：保持全零 → 关闭视图 → 等中立 `120 ms` → 仅恢复 `IMMERSIVE_MENU` 自己创建的暂停 →
交还游戏输入。若打开前 Core 已暂停，关闭菜单不得越权恢复。

### 11.4 退出和错误

- 退出不创建存档，也不上传额外状态；菜单文字固定说明“退出不会保存当前进度”。
- 默认选择继续，因此不能由打开菜单的同一组合直接退出。
- finish 请求失败时保留菜单和 Core 暂停，显示“退出失败，A 重试 / B 继续”，不能先卸载 Player。
- Core 致命错误默认动作返回原游戏列表；保留原 Game 选择。
- 普通 PC/移动 Player 不识别双 chord，也不隐藏现有工具栏。

## 12. 只读 API 设计

现有 `/api/v1/home` 没有每个平台最近游玩时间，现有 `/api/v1/games` 不投影 VIDEO/description；用全量
`recent-games` 客户端分组或每移动一次追加详情请求都会形成过量数据/请求。因此实施时新增两个只读、
Profile 隔离的用户 API，不修改数据库。

### 12.1 `GET /api/v1/immersive/platforms`

响应：

```json
{
  "generatedAtMs": 1787580000000,
  "items": [
    {
      "platformId": "arcade",
      "platformName": "Arcade",
      "gameCount": 24,
      "lastPlayedAtMs": 1787578200000
    }
  ]
}
```

契约：

- 只统计 enabled、未删除 PlatformInstance 下 `PUBLISHED` Game；
- 只返回 `gameCount > 0` 的基础平台；
- `lastPlayedAtMs` 是当前 Profile 在该平台所有 PlaySession 的最大 `started_at_ms`，可空；
- 排序为平台全名 case-insensitive、平台 ID；
- `generatedAtMs` 是所有相对时间文案的统一服务器时钟；
- 不返回游戏目录、ROM、Core hash、设备或其他 Profile 数据。

### 12.2 `GET /api/v1/immersive/platforms/{platformId}/games`

Query：`cursor` 可空，`limit` 默认/最大均为 50，不接受搜索和排序参数。

响应：

```json
{
  "generatedAtMs": 1787580000000,
  "platform": {
    "platformId": "arcade",
    "platformName": "Arcade",
    "gameCount": 24,
    "lastPlayedAtMs": 1787578200000
  },
  "items": [
    {
      "gameId": "00000000-0000-7000-8000-000000000000",
      "title": "打击者1945加强版",
      "description": "…",
      "releaseYear": 1999,
      "developer": "Psikyo",
      "genre": "飞行射击",
      "platformInstance": {"id": "…", "name": "FBNeo 游戏"},
      "defaultCore": {"id": "fbneo", "name": "FinalBurn Neo"},
      "coverUrl": "/content/assets/…",
      "videoUrl": "/content/assets/…",
      "lastPlayedAtMs": 1787578200000
    }
  ],
  "nextCursor": null
}
```

契约：

- 可见性与平台聚合完全一致；不存在、无可见游戏或已停用平台统一 404，不泄漏历史存在性；
- 标题排序为规范化 title case-insensitive、Game ID；cursor 绑定 Profile、platform、sort 和 limit；
- COVER/VIDEO 只取当前 MetadataRevision 各自 ordinal 0 的 Asset 逻辑 URL；不存在为 null；
- description/developer/genre 等来自同一 MetadataRevision，不能跨 revision 拼接；
- URL 受现有登录和 Game 可见性保护，媒体移除或游戏删除后按现有契约 404；
- 响应不内嵌媒体 bytes、不返回 Blob ID/hash/宿主路径；
- 该路由是“游戏详情之外允许读取 VIDEO 的第二个用户投影”，实施时必须同步修改正式架构和 HTTP 契约。

### 12.3 写 API 和 migration

- 不新增写 API；
- Launch 继续使用现有 `POST /api/v1/launches`；
- 不新增 DB 表、字段或 migration；
- 不持久化沉浸模式、焦点、活动手柄、视频播放位置或主题；
- 新 route 必须先更新 OpenAPI、生成 TypeScript schema，并接受严格 query allowlist。

## 13. 前端模块划分

| 职责 | 建议位置 | 约束 |
| --- | --- | --- |
| Gamepad 快照纯模型 | `web/features/immersive/input-model.ts` | edge、axis、repeat、neutral、double chord；fake clock 单测 |
| 浏览器输入源 | `web/features/immersive/gamepad-source.ts` | Gamepad API、rAF、visibility、cleanup，可注入测试源 |
| 首页入口 | `web/features/immersive/entry-dialog.tsx` | 只挂首页；复用现有 Dialog，不污染 App Shell |
| 独立 Shell | `web/features/immersive/immersive-shell.tsx` | 路由、输入所有权、帮助条、全视口背景 |
| 平台视图 | `web/features/immersive/platform-view.tsx` | 横向 carousel、恢复、空错状态 |
| 游戏列表 | `web/features/immersive/game-list-view.tsx` | 分页、选择、媒体/简介、Launch |
| 媒体模型 | `web/features/immersive/media-stage.tsx` | 单 video、延迟、取消、回退、visibility |
| API client | `web/features/immersive/api.ts` | generated schema、AbortSignal、cursor，无手写重复 DTO |
| Player 菜单模型 | `web/features/player/immersive-controls.ts` | 组合、输入过滤、暂停所有权、退出 |
| Player 菜单视图 | `web/features/player/immersive-menu.tsx` | 两按钮、独立焦点、全零门禁 |
| adapter 集成 | `web/features/player/adapters/*` | 在 loader 首次轮询前安装，teardown 恢复 |

复杂状态不得塞入页面 JSX。生产和 E2E 使用同一高层状态机；测试只替换 GamepadSource，不直接调用页面
handler、Launch 成功回调或 Player 菜单函数。

## 14. Player adapter 和机器契约

EmulatorJS 会在 iframe 中直接读取 `navigator.getGamepads()`。只在 React host 监听无法保证 Select/Start
不进入 Core，因此过滤器必须在 loader 加载和首次 Gamepad 轮询之前安装，并在 teardown 恢复。

实施时普通 Player adapter ID 固定升级：

| 当前 ID | 新 ID | 原因 |
| --- | --- | --- |
| `ejs-4.2.3-v2` | `ejs-4.2.3-v3` | 增加可选沉浸 Gamepad 过滤与菜单全零所有权 |
| `ejs-4.3.0-pre-v1` | `ejs-4.3.0-pre-v2` | 同一契约在预览版本独立绑定 |

联机明确不在范围，`ejs-netplay-4.2.3-v1` 与 `data/netplay/v2` profile 不升级。新的普通 adapter 在
`experience !== immersive` 时逐对象保持现有 Gamepad 行为；未知 adapter/version fail closed，不尝试默认实现。

必须同步：

- 两个 EmulatorJS manifest 的 `player_adapter.id`；
- `web/features/player/adapters/registry.json` 与显式 adapter 实现；
- OpenAPI/生成 TypeScript 中的普通 adapter 枚举；
- Go Launch/profile 常量及依赖双向检查；
- `data-check`、`prepare-deps`、`deps-check` 和 release-input digest；
- 普通 Player、DOS、Arcade、多盘与全部 35 core 的既有运行回归。

不修改物化的 EmulatorJS/Core payload，不把测试开关放入生产 URL。

## 15. 视觉系统

### 15.1 方向

- 背景：近黑蓝 `#080b14` 到 `#151b2c` 的低亮渐变，叠加非常轻的扫描线纹理；
- 主文字：`#f7f5f0`，辅助文字 `#aeb7ca`；
- 主焦点：Retrom 紫 `#7560ff`，运行辅助青 `#50d6ca`；
- 危险退出：只在已选中时使用低饱和红，不让它成为默认视觉中心；
- 平台卡圆角 `28px`，游戏媒体圆角 `18px`；电视距离下正文不低于 `20px`；
- 不使用像素字体、外部主题字体、霓虹泛光墙或第三方平台 Logo；
- 动画只表达层级和选择，不使用持续漂浮、粒子或大范围视差。

### 15.2 支持视口

- 正式沉浸布局：横屏且 CSS viewport 至少 `960×540`；
- 必验：`1280×720`、`1920×1080`、`2560×1440`、物理 4K 150%（CSS `2560×1440`, DPR 1.5）；
- 超宽屏保持中央 `max-width: 2200px`，背景铺满，内容不被拉到两端；
- 小于门槛或竖屏显示阻断页：“沉浸模式需要横屏大屏”，B 返回普通首页；不重排成普通移动 UI；
- `100dvh`、safe-area 和浏览器缩放 80%–200% 下不得裁掉帮助条或当前选择。

### 15.3 焦点和可访问性

- 当前选择必须同时有位置、边框、大小/字重和文本状态，不能只靠颜色；
- 底部帮助条使用标准位置图标和 A/B 文字，不猜 PlayStation 的物理标签反转；
- 页面有唯一 H1；列表使用 listbox/option 或等价 roving tabindex，当前项有 `aria-selected`；
- 状态和错误使用 `aria-live`，方向重复不逐项播报高频噪声；
- `prefers-reduced-motion` 关闭 carousel、媒体淡入和视频自动播放；
- 键盘 Tab 仍可到达显式按钮，Enter/Escape 行为与 A/B 一致；
- 所有可点击目标至少 `48×48px`。

## 16. 数据竞争、错误和恢复

| 场景 | 规定行为 |
| --- | --- |
| 首页按键触发时手柄断开 | 关闭/保持普通首页，并提示“手柄已断开” |
| 多手柄同时按键 | 最先观察到有效 edge 的 index 获得认领；同帧按 index 小者稳定兜底 |
| 平台在浏览期间变空/停用 | 游戏 API 返回 404；回平台视图刷新并提示“该平台暂时没有可游玩的游戏” |
| 当前游戏被删除 | 移到相邻游戏；无相邻项则回平台视图 |
| 媒体被替换/回收 | 旧 URL 404 后回退占位；不重试旧 URL |
| VIDEO 解码失败 | 保留 COVER，记录低基数客户端诊断，不阻断启动 |
| 分页失败 | 保留已加载项，在尾项提供手柄可触发重试 |
| Launch validation pending | 同一加载层等待；不创建第二个 Launch 按钮 |
| Launch blocker | 留在选中游戏，显示普通语言错误；B 关闭 |
| Player 菜单 pause 失败 | 不显示“已暂停”，保持输入全零并给出重试/返回列表 |
| exit finish 失败 | 保留暂停菜单，不卸载 iframe；A 重试，B 继续 |
| 页面隐藏 | 停止 UI repeat/video；Player 依现有单机可见性规则记录时长，并释放 pending 组合 |
| 登录失效 | 清空媒体和列表，跳转登录；不显示上一账号的 Profile 时间 |

## 17. 测试与验收方案

### 17.1 纯逻辑单元测试

至少覆盖：

- standard mapping、异常值、任意 button edge、同帧多手柄稳定认领；
- 入口按键被消费、`120 ms` 中立、取消后的 `500 ms` 重新武装；
- 摇杆 `0.60/0.35` 回滞、方向重复 `350/120`、反向和失焦清理；
- 平台左右循环、游戏首尾不循环、焦点恢复和已删除项相邻回退；
- cursor 预取只并发一个请求、失败重试、过期响应不能覆盖新平台；
- 视频 `700 ms` 延迟、单实例、selection/hidden/reduced-motion/error 清理；
- 两次 chord 的 `100/60/650 ms` 边界、单 Select/Start 脉冲、单 chord 抑制；
- 菜单打开 release、全零、暂停所有权、关闭中立和 exit 失败恢复。

全部时序使用 fake clock，不读取真实硬件或真实时间。

### 17.2 后端/API 集成

- Profile A/B 的平台 `lastPlayedAtMs` 隔离；
- 只统计 published + enabled，零游戏平台不出现；
- 平台和游戏稳定排序、cursor 不重不漏、未知 query 拒绝；
- description/COVER/VIDEO 来自同一 current MetadataRevision；
- 游戏/媒体替换、停用和硬删除后投影/URL 立即变化；
- 404 不泄漏已停用、其他账号私有时间或墓碑资产；
- API schema、middleware allowlist、认证和生成 client 一致。

### 17.3 组件和视觉测试

- 首页进入 Dialog：任意按钮只开框，默认取消，左右+A、B 和鼠标/键盘语义一致；
- 独立 ImmersiveShell 不渲染 PC sidebar/mobile nav；
- 平台计数/最近时间、carousel、空错和退出确认；
- 游戏列表的左右布局、有/无 COVER、VIDEO、description、重复标题、分页失败；
- 1280×720、1920×1080、2560×1440、4K 150% 无裁切/溢出；
- reduced-motion、键盘、焦点、axe serious/critical 为零；
- 普通 PC/移动首页和 Player 无视觉/交互回归。

### 17.4 全流程 Chrome E2E

新增独立 Case，不与普通 Player Case 合并：

| Case | 上限 | 路径与硬断言 |
| --- | ---: | --- |
| `ACC-IMM-001` 入口与隔离 | 90s | 首页任意按键→默认取消→B 留首页；再次触发→左右+A进入；普通其他页不响应 |
| `ACC-IMM-002` 平台选择 | 120s | 真实 API 计数/最近时间；左右循环；A 进入；B 退出确认；独立壳无普通导航 |
| `ACC-IMM-003` 游戏媒体列表 | 150s | 上下选择；COVER+VIDEO+description；无媒体回退；单 VIDEO；分页和 B 恢复平台 |
| `ACC-IMM-004` 单机游玩闭环 | 180s | 项目自有 GBA ROM 经真实 Launch/content/Player；Core 非黑帧；退出回原 Game |
| `ACC-IMM-005` 暂停菜单与输入隔离 | 180s | 单 Select/Start 到 Core；单 chord 抑制不打开；双 chord 暂停；默认继续；退出 finish |
| `ACC-IMM-006` Arcade 与多输入回归 | 180s | 项目自有 Arcade ROM 投币/Start；第二手柄游戏输入；活动 pad 菜单；无粘键 |
| `ACC-IMM-007` 状态与无障碍 | 150s | 断开、隐藏、删除/媒体漂移、错误恢复、reduced-motion、axe、四 viewport |
| `ACC-IMM-008` adapter 与普通 UI 回归 | 240s | 新普通 adapter IDs/manifest/OpenAPI；35 core 既有链路；普通 PC/移动/联机不变 |

E2E 使用可注入的 GamepadSource 驱动真实 DOM、API、Launch、content endpoint、Player 和 Core；不得直接调用
React handler 或伪造 Launch/Core 成功。GBA/Arcade 使用仓库项目自有公开 fixture。VIDEO 使用同一公开
EmulationStation GBA fixture 的项目自有 WebM，不能读取 `/data/game` 或操作者私有 ROM。

实体 smoke 至少使用一只 Chrome 报告 standard mapping 的手柄完成：进入 Dialog → 选平台 → 选游戏 →
运行 GBA/Arcade → 单独 Select/Start → 双 chord 菜单 → 继续 → 再开菜单退出。它只记录匿名类别、mapping、
button/axis 数量和结果，不保存设备 ID。没有实体设备时自动化可 PASS，但发布验收状态必须 BLOCKED。

### 17.5 必跑门禁

```text
make quality-structure-check
make api-generate
make api-check
make fmt-check
make build
make test
make lint-go
make integration-test
make web-install
make web-lint
make web-typecheck
make web-test
make web-build
make public-fixtures-check
make data-check
make prepare-deps
make deps-check
make web-e2e
make ci
```

adapter/manifest 变化还要校验 release-input digest；若镜像发布内容受影响，运行 `make build-images`。必须复跑
普通 GBA、DOS、Arcade、多盘和 `ACC-NP-014`–`022`，证明普通/联机 Player 没有继承沉浸过滤行为。

## 18. 实施顺序

1. 把本方案稳定决策合并到架构、UI、运行时、HTTP、依赖、质量和验收正式文档；
2. 更新统一设计源并导出正式 HTML，不能把本临时 HTML 复制成生产设计源；
3. 先改 OpenAPI，增加两个只读 route/DTO，生成 client；
4. 实现后端 Profile 隔离聚合、分页和集成测试；
5. 实现纯输入模型、浏览器源和单元测试；
6. 实现首页入口 Dialog 与独立 ImmersiveShell；
7. 实现平台视图、游戏列表、媒体时序和分页；
8. 实现沉浸 Launch、Player 两按钮菜单、双 chord 和 adapter 过滤；
9. 同步 manifest/registry/常量/生成物和依赖检查；
10. 补齐组件、API、产品 E2E、真实手柄 smoke 与所有回归；
11. 所有门禁通过后删除本临时目录。

未完成 adapter 输入隔离前，不得打开用户可见入口；避免出现 UI 能进但 Select/Start 同时泄漏到游戏的
半成品。可以在提交间使用编译期内部开关，但生产构建不保留 URL/query 测试开关或未完成入口。

## 19. 完成定义

只有以下条件全部满足，002 才算完成：

- 普通首页任意标准手柄按钮能安全触发进入确认，取消与确认均可只用手柄完成；
- 沉浸模式拥有独立 UI 壳，普通 PC/移动导航不进入该壳；
- 用户可只用手柄完成选择平台、选择游戏、真实启动和返回游戏列表；
- 平台游戏数和当前 Profile 最近时间准确，游戏媒体/简介来自同一当前 metadata revision；
- COVER/VIDEO 缺失、失败、替换和删除都有确定性回退；
- 双击 Select+Start 时序、Core 输入抑制、暂停、继续和退出没有粘键或穿透；
- 沉浸菜单不包含存档、联机或其他越界功能；
- 普通 PC/移动、普通 Player、联机 Player 和全部已登记核心无回归；
- 新 API、OpenAPI、生成物、普通 adapter ID、manifest 和 registry 完全一致；
- `ACC-IMM-001`–`008`、适用既有 Case、全门禁和实体 smoke 当次通过；
- 稳定决策已合并到正式文档和统一设计源；
- `td/002-immersive-mode/` 已删除且仓库没有对其引用。

缺少实体手柄可以明确报告 BLOCKED，但不能删除临时方案、不能把自动注入 E2E 描述成实体兼容通过。
