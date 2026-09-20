# 项目记忆（AGENTS.md）

> 本文件由 DSH 自动加载，是本工作区跨会话的持久记忆。同时兼容 Claude Code（AGENTS.md 约定）。
> 来源：Claude Code 记忆 `~/.claude/projects/-Users-shoushenwei-GolandProjects-awesomeProject/memory/`（teaching-plan.md / user-profile.md），接入于 2026-08-19。

## 项目性质

这是一个**教学项目**：目标是借"多点路线自动规划"业务，让用户从零学会 Go 和中间件/组件。业务是载体，教学是目的——**所有工作都要边做边讲，不能闷头做完**。

完整设计见 `docs/superpowers/specs/2026-08-13-route-planner-design.md`。

## 用户画像

- 用户是 Go 语言初学者（非零基础，已有 GoLand 环境，go 1.26），目标是通过"路线规划"项目系统学习 Go 和中间件（gin、Redis、MySQL、Redis Stream、gRPC）
- 全程中文交流，偏好动手实践、边做边讲、讲解"为什么"
- 学习价值优先于交付速度

**每次会话的固定动作：**
- 开始：回忆本记忆，从"进度追踪/下次继续"处接续教学，先讲概念再动手
- 过程中：讲解每一步的原理，不只给代码；允许拆成小步骤逐步推进
- 结束：更新"进度追踪"（当前阶段/已教内容/下次继续），保证下次能接上

## 课程阶段（顺序即学习顺序）

- **Phase 0 环境与工程初始化**（完成）：go module、git、.gitignore、标准库 net/http vs gin 的区别
- **Phase 1 MVP**（进行中）：gin + 高德矩阵 API + 手写 TSP + Redis 缓存 + 简单 HTML 页面
  - 手写 TSP = 最近邻起步 → 2-opt → 模拟退火，逐步看解变好
- **Phase 2 异步 + DB**（未开始）：Redis Stream 消费组 + MySQL 持久化 + Repository 模式
- **Phase 3 微服务拆分**（未开始）：api / matrix / solver 三个 gRPC 服务

## 关键决策（已确认，不要改）

- 高德地图 API（免费额度够学习）
- 手写 TSP（不引 OR-Tools，cgo 太重）
- Redis Streams（不引 Kafka）
- MySQL docker + database/sql（不引 ORM）
- API + 简单 HTML 页面（不做完整前端）
- 单进程演进 → 再拆 gRPC（方案 A）
- 端口 **7800**（用户指定，避免冲突）
- **前端先行、前后端分离**：页面是独立前端，gin NoRoute 托管 `web/`，与 API 同源无 CORS
- **前端 = React 19 + Vite**（2026-09-14 定稿）：源码 `frontend/`，构建产物输出到 `web/`
  （`vite.config.js` 里 `base:'/'` + `outDir:'../web'`，**这两个必须同指一处**，只改一个就 404），
  由 Go 托管在**站点根路径 `/`**。
  **手写原生 JS 版（`web/app.js` + `web/index.html` + SortableJS）已删除** ——
  它踩过的坑（`renderList()` 全量重建导致拖拽库引用失效、删点要同步两个平行数组）
  记在本文档 2026-08-20 条目里，是"React 用 key + 单一数据源解决了什么"的活教材
- **视觉基准**：墨色 `#1D1D1F` = 唯一交互色；蓝 `#0066CC` = 只给「驾车」语义
  （绿=步行、橙=公交）。设计变量在 `frontend/src/styles/tokens.css` + `frontend/src/theme.js`
- **路线绘制**：前端调 `/route` 拿真实路网 polyline；拿不到（公交无轨迹 / 请求失败）时该段退化为直线
- **开源：key 由使用者自配**——前端 JS key 存浏览器 localStorage（设置面板可换，进 settingsOverlay）；后端将来用的高德 Web 服务 key 走环境变量 AMAP_KEY
  - ⚠️ **高德的两类 key 不能混用**（按「服务平台」划分）：前端地图要 **Web端(JS API)**，
    后端 REST 要 **Web服务**。同一个 key 可在控制台**追加多个平台**，不必申请两把。
    报错码：`INVALID_USER_KEY`(10001)=key 不存在；`USERKEY_PLAT_NOMATCH`(10009)=平台不对；
    `INVALID_USER_SCODE`=缺**安全密钥**（2021-12-02 后申请的 key，JS API 2.0 强制要求，
    **纯地图渲染不需要它，但凡要调接口的能力都需要**）
  - ⚠️ **`AMAP` 就是高德**（AutoNavi Map），环境变量叫 `AMAP_KEY` 容易让人以为要接第二个厂商 ——
    用户 2026-09-14 就踩了这个理解坑，文档/UI 里凡提到 key 都要顺手讲清"只有高德一家"

## 进度追踪

### 2026-09-19（DSH 会话）— 手写 ReAct 智能体循环打通（未接真 LLM）✅

用户想用 Go 构建 agent（对比过 Python deepagents，定案：留在 Go 技术栈、
先手写循环学原理，之后可对照 Eino 的 deep 包）。**本轮只通循环，未配大模型 key**。

- **新增 `internal/agent/`**（全部中文教学注释）：
  - `types.go`：Message/ToolCall/ToolSpec/Completion。Content 用 `*string`
    区分 null 与空串（OpenAI 协议里 assistant 带工具调用时 content 是 null）
  - `model.go`：`Model` 接口（循环只依赖它，测试塞假模型零成本）+
    `OpenAICompatible` 客户端——DeepSeek/Kimi/Qwen 兼容模式/Ollama 通吃，
    换厂商只改 BaseURL+Model。超时 60s（LLM 慢，和高德的 5s 不同档）
  - `tool.go`：`Tool` 接口（Spec 给模型看 / Run 真执行），`parseInput` 收口参数校验
  - `loop.go`：**ReAct 循环本体**。核心 4 行：调模型 → 无工具调用=出口 →
    并发 goroutine 执行工具 → 结果按 ToolCallID 回填历史再循环。
    防御件：MaxIterations 刹车（默认 10）、未知工具/工具报错转错误文本喂回
    （模型可自救，不中断）、OnStep 回调（实时观察每轮）
  - `tools_plan.go`：业务工具 ×2 —— `search_place`（包 amap.SearchPlaces）、
    `plan_route`（直通 planner.Compute，校验单一真相源，零复制）。
    工具层刻意薄：同步接口/异步 worker/agent 三方跑同一条解算链
- **测试 `loop_test.go`**：fakeModel（脚本化逐轮应答+记录收到的消息）
  + echoTool（带锁，-race 抓过一次并发写——反证并发路径真在并发）。
  4 个场景：两轮标准流 / 未知工具+坏参数喂回 / MaxIterations 刹车 / 并发调用对位。
  变异验证过牙齿（弄坏"结果喂回"立刻红）
- **`cmd/agent-demo/main.go`**：独立演示入口（和 7800 服务互不干扰）。
  运行：`set -a; source .env; set +a` + `LLM_BASE_URL/LLM_API_KEY/LLM_MODEL`
  三个变量（任选 OpenAI 兼容厂商），`go run ./cmd/agent-demo "问题"`
- **⏭️ 下次继续**：① 用户配 LLM key 实测真模型（观察它会怎么组合
  search_place→plan_route）；② 对照 Eino deep 包看框架版多了什么
  （todo 计划/文件系统工具/子代理）；③ 可选：agent 挂到 /api/agent 端点
  （要过 auth）或接 Phase 2 的 Stream worker 做异步 agent 任务

### 2026-09-14（DSH 会话 · 三续）— 排查"搜索为什么不管用" + 订正会误导人的文案 ✅

用户原话："为什么搜索地址不管用呢，是因为 amap 的 api 一定要，不能走高德的 api 吗"
→ 暴露了一个**理解坑：以为 AMAP 和高德是两个厂商**（AMAP 就是高德 / AutoNavi Map）。

- **三层实测**（浏览器注入 key + curl，结论有据可查）：
  | 能力 | 需要 | 结果 |
  |---|---|---|
  | 地图渲染 | JS key | ✅ `typeof AMap==='object'`、canvas 已创建 → key 有效、localhost 在域名白名单 |
  | 前端直连搜索 `AMap.PlaceSearch` | JS key **+ 安全密钥** | ❌ `INVALID_USER_SCODE`（`_AMapSecurityConfig` 为 null） |
  | 后端 `/search`（REST） | Web服务平台的 key | ❌ 503「未配置 AMAP_KEY」（进程环境里 0 条）+ 10009 |
- **修了 3 处"承诺了做不到的事"的文案** ⭐（都是这次卡点的直接原因）：
  - `SettingsDialog.jsx`：安全密钥标签去掉「（可选）」、placeholder 改「必填」；
    hint 重写成三句：这个框只管浏览器端地图 / 安全密钥为什么不能留空 / 后端另需 Web服务 key（两把可合成一把）
  - `MapView.jsx` 无 key 空态：原文"填入后即可查看真实路网与驾车轨迹"是**错的**——
    真实路网靠后端 `/route`，要的是服务端那把 key；改成"填 JS key 显示地图 / 真实路网另配 AMAP_KEY"
  - `api.js`：新增 `ERROR_HINTS` + `withHint()`，把后端「未配置 AMAP_KEY,搜索不可用」
    补成"……后端要的是 Web服务平台的 key，不是浏览器里那把 JS key（见 README）"——
    **错误信息要能被行动**，否则用户只知道"缺东西"、不知道去哪儿补
- **`README.md`**：§1 重写（**一把 key 勾两个平台即可，不必申请两把** / 安全密钥必填的欺骗性现象 /
  「报错码 → 病因」表）；新增 **「❓ 排查」** 一节（5 条 现象→原因→修法，含"换浏览器 JS key 就没了"）
- `architecture-diagrams.md` 技术栈表补安全密钥；本文件「关键决策」补 `INVALID_USER_SCODE` 与 AMAP 命名澄清
- **验证**：vite build 82ms；浏览器实测弹层 378px < 视口 577px（不溢出）、
  `.dialog-hint strong` 计算 font-weight = 700、搜索失败 toast 实测输出带补全文案

**⏭️ 下次继续（接手清单，别重新排查一遍）**

0. **2026-09-15 已完成（见 memory/2026-09-15.md 详情）**：① `drivingMatrix` 逐列并发
   （`maxColumnConcurrent=4` 信号量限流 + 教学注释，3 个新测试 + 2 个旧测试并发适配，
   `-race` 全绿 + 变异验证）；② 前端"自动模式"重做成 radio 卡片并改默认自动
   （原功能一直存在，被"默认手动 + 反向 checkbox"埋没）。**并发改造下一步 = context 贯穿**；
   `maxColumnConcurrent` 等 key 加上 Web服务 平台后按真实 QPS 调参

1. **用户待办 · 控制台**：给 key `2d47…2c61` 编辑 → 勾选 **「Web服务」** 平台（**保留 JS API 不动**）→ 提交。
   key 值不变，不用改代码；「安全密钥」也可顺手一起复制
2. **然后重启服务**：`set -a; source .env; set +a; go run .`（`.env` 已在、已被 gitignore；
   上次跑的服务**没加载 `.env`**，所以 `AMAP_KEY` 在进程环境里是 0 条 → 什么都会报"未配置"）
3. **跑冒烟**：`./scripts/smoke.sh` —— 12 条断言，含"3 固定点驾车真实路网 ≈45km vs 直线 ≈29km"
   这个**识破假降级**的判据。无 key 模式下它是 6 通过 6 失败，失败项应精确指向"未配置 AMAP_KEY"
4. **补测 6 项**（上轮因无 key 跳过）：地图渲染与地图点选 · 搜索成功候选 · `/route` 真实路网绘制 ·
   `is_degraded` 降级警告条 · 图例显示 · `setFitView` 缩放
5. **环境现状**：Redis 6379 **未启动**（二进制在 `.redis-src/redis-7.2.5/src/redis-server`）；
   MySQL 容器 `routeplanner-mysql` 仍在跑（3306 / 库 `routeplanner`）；7800 已释放
6. **积压未提交**：约 10 个修改文件 + 3 个暂存删除（`web/index.html`、`app.js`、`vendor/`）

### 2026-09-14（DSH 会话 · 下半场续）— React 版功能实测 + 删旧版 + 根路径接管 ✅

用户原话："直接启动 react 版本的，然后测试功能，功能没问题的话旧版的前端可以删了"。

- **浏览器实测（真实 Chromium，14 项全过）**：页面渲染 · 手动输入坐标加点 · 列表编号 ·
  每段出行方式切换 · 手动/自动模式切换 · **拖拽排序**（天坛拖到首位，顺序正确变化且点数不变）·
  拖拽连带行为（badge 重编号 · **旧结果自动作废** · legMode 重置）· 删除点 ·
  自动模式 TSP（天安门→故宫→天坛 **4.7 km**，与后端实测一致）·
  13 点 TSP（**159.2 km**，顺序明显重排）· 手动模式 503 → **就地错误条** ·
  搜索失败 → **toast**（替代旧版 `alert()`）· 设置弹层（开 / 取消 / **Esc 关闭**）·
  空 key **字段级校验**（弹层不关、不写 localStorage）·
  **点数上限分档**（13 点手动模式按钮禁用 + 给出原因，切自动模式立刻放行）· 地图无 key 空态
- **未测（都依赖 key）**：地图渲染与地图点选、搜索成功路径、`/route` 真实路网绘制、
  降级警告（`is_degraded`）、图例 / 缩放 / `setFitView`
- **删旧版 + 根路径接管** ⭐：
  - `vite.config.js`：`base: '/app/'` → `'/'`，`outDir: '../web/app'` → `'../web'`
    —— **base 与 outDir 是一对，必须同指一处**（base 决定 HTML 里资源路径怎么写，
    outDir 决定文件放哪；只改一个就"页面能开、JS 全 404"）
  - 重新构建（101ms）→ `web/` 下只剩 `index.html` + `assets/`；
    `emptyOutDir` 顺便清掉了旧的 `index.html` / `app.js` / `vendor/`
  - 实测：`/` → **200**（React 版）· `/assets/*.js|css` → **200** · 旧路径 `/app/` → **404**（预期）
  - `.gitignore`：`/web/app/` → **`/web/`**（整个目录都是产物）；
    `frontend/.gitignore` 里的 `dist/` 是**无效规则**（outDir 不在那），改成说明"产物在 `../web`"
  - 旧版文件备份在 `/tmp/rp-oldfrontend/`；git 里也能恢复（`git checkout -- web/`）
- **测试工具踩坑**（可复用）：
  - `agent-browser drag` **驱动不了 HTML5 DnD**——它内部用鼠标事件模拟，不触发 `dragstart`。
    正确姿势是原生 `DragEvent` 派发：`new DataTransfer()` +
    依次 `dragstart` / `dragover`(要 `preventDefault`) / `drop` / `dragend`
  - **元素在视口外时 `fill`/`click` 会"返回 ✓ Done 但什么也没发生"**。
    批量填 React 受控输入用 `HTMLInputElement.prototype.value` 的 setter + 派发 `input` 事件
  - 安装：`npm i -g agent-browser && agent-browser install`（会下 182MB Chrome，约 1 分钟）
- **macOS 坑（重要）**：BSD `grep` **不支持 `\|`** 这种 BRE 写法（会被当字面量 → 假 0 结果），
  必须用 `grep -E`。本会话据此修正过一次错误结论

### 2026-09-14（DSH 会话 · 下半场）— React 版前端（用户要求「用 React 实现这个」）✅

用户想**同步学 React**，所以这一轮不只是改前端，而是"用 React 重写一遍"作为对照。

- **新增 `frontend/`**：React 19.3 + Vite 8.3，纯 JSX（不上 TS，先专注 React 本身）
- **构建产物输出到 `web/app/`，Go 一行都没改** ⭐
  `router.go` 的 `NoRoute(http.FileServer(http.Dir("web")))` 托管的是整个 `web/` 目录，
  所以产物落进 `web/app/`，React 版自动出现在 `/app/`。
  **旧手写版留在 `/`，两版并存可对照** —— 这是本次最省事的集成方式
- **开发体验**：`vite.config.js` 里 `base: '/app/'` + `server.proxy` 把
  `/plan` `/search` `/route` 代理到 7800。开发时跑 `npm run dev`（5173，带 HMR），
  后端照常 `go run .`，两个进程同时跑
- **构建产物 gitignore**（`/web/app/`、`/frontend/node_modules/`）：产物能由源码再生，不进仓库

#### 组件拆分（`frontend/src/`）

```
App.jsx（组合根：持有 points/manual/key，定义业务规则，摆版面）
├── TopBar / ControlPanel / MapView / SettingsDialog / ToastStack
components: ControlPanel → SearchModule / PointList(+LegRow) / PlanOptions /
            PlanAction / ResultCard
hooks:      usePoints(增删改排) · useAmap(地图生命周期) · usePlan(异步链路+阶段)
            useSettings(key 持久化) · useToasts(提示条)
styles:     tokens.css(设计变量) + app.css(组件样式)
theme.js:   设计变量的 JS 侧镜像（地图折线读不到 CSS 变量）
```

#### 解决了手写版的哪些问题（这是这轮的核心价值）

| 手写版的问题 | React 版 |
|---|---|
| `renderList()` 全量重建 `<li>` → Sortable 引用失效 → 必须 `destroy()` 再 `new` + `initSortable()` 可重入 | **不需要了**。React 靠 `key` 认出节点身份，重排是**移动**而不是重建，第三方持有者不会失效 |
| 删点要同时 `splice` `points` 和 `legs` 两个平行数组，忘一个就错位 | `legMode` 挂在点上，"每段方式"成为点自带的属性，只剩一个数组 |
| 错误全靠 `alert()` | 字段级内联错误（手动坐标）+ 就地错误条（规划失败）+ 顶部提示条（搜索失败），**三者按"用户需不需要对着它办事"分工** |
| 无 loading / 无进度 | `phase` 状态机 `idle → submitting → drawing → done`，如实驱动三段进度 |
| `esc()` 防 XSS | 消失。JSX 的 `{变量}` 天然是文本节点（和 Go `html/template` 同一类保障） |
| 颜色写三份（CSS / `modeColor()` / 图例） | `tokens.css`(CSS 侧) + `theme.js`(JS 侧) 单一来源，且 `theme.js` 在 dev 下会**运行时断言**两者一致 |
| 主色 `#1a73e8` 与「驾车」语义色撞车 | 墨色 `#1D1D1F` = 唯一交互色；蓝色只留给「驾车」 |
| SortableJS 依赖（本地托管 40KB） | 去掉，改用原生 HTML5 DnD + state，**零依赖** |

#### 踩坑记录（都写进代码注释了）

- ⭐ **跨事件传值不要只放 state**：拖拽的 `dragIndex` 若只用 `useState`，
  `drop` 处理器可能读到旧闭包里的 `null`。真实拖拽时中间有 `dragover` 触发的重渲染
  "碰巧"掩盖了它 —— **正确做法是放 `useRef`**（写入立即生效），state 只管视觉。
  这个问题是自动化测试抓出来的，人工点鼠标测不出来
- **`useAmap` 要拆两个 effect**：创建地图的 effect 的 cleanup 拿不到刚建好的实例
  （闭包捕获的是 effect 运行那一刻的值）。销毁必须单独写一个依赖 `[map]` 的 effect
- **`<ul>` 里只放 `<li>`**：把"段连接线"塞进同一个 `li` 的内部，
  拖拽下标的换算就消失了（手写版正是把连接线插在中间，才必须改用 `oldDraggableIndex`）
- **地图覆盖物没有增量接口**：只能"全删重画"。十几个点无所谓，
  几千个点就得自己写差集（React 的 diff 只管它渲染的 DOM，管不到第三方库内部对象）

#### 验证方式

用 `puppeteer-core` 驱动本机 Chrome 跑了 12 条断言，**全部通过**：
空态不误报错 · 加 3 点 · **拖拽重排后顺序与起点徽标同步** · 拖回原顺序 ·
字段级校验拦截 · 规划失败就地报错条 · 关掉手动模式后规划成功（5.1 km）·
成功后错误条自动清除 · 搜索失败弹提示条 · 只剩 1 点时按钮禁用并给原因。
控制台除 4 条预期的 503 网络日志外无任何 React 警告（StrictMode 下通过 = 清理函数干净）。

**遗留**：①React 版目前只跑了无 key 模式，有 key 时的地图渲染/折线未实测
②`web/app/` 是产物，`go run .` 前需先 `npm run build`
③两个前端并存的迁移收尾（想切换成 React 单版就把 `base` 改 `/`、`outDir` 改 `../web`）

### 2026-09-14（DSH 会话 · 上半场）— 代码审查 + 修复 12 个问题 + api 层测试补齐 ✅

- **一次通读式代码审查**（代码 + docs 两份文档 + README + smoke.sh），发现并**修复**：
  - ⭐ **`/plan` 不校验 `mode`，非法值静默降级**（最严重）：`mode=cycling` → amap 报
    `unsupported mode` → matrix 吞掉降级 → **返回 200 + haversine 直线 + `is_degraded=false`**。
    用临时 httptest 探针实测确认（`total_km=15.1055`）。修法：新增 `parseMode()` 白名单，
    `/plan` 与 `/route` 共用，「同一个参数两个接口一套标准」
  - ⭐ **`is_degraded` 只说了一半的降级**：`matrix.DistanceMatrix` 签名从 `([][]float64)`
    改成 `([][]float64, bool)`，把"这张矩阵是降级来的"带出包外；`IsDegraded` 改为
    `len(warnings) > 0`（单一真相源）。**教学点：降级要上报，就必须进返回值——日志在服务器上，用户看不到**
  - ⭐ **前端按名字回查坐标 → 重名地点画错线**：后端新增 `order_idx`（下标数组），
    前端 `drawRoute(orderIdx)` 改用下标，删掉 `pointByName`。**教学点：名字不是标识符**
  - **点数上限分档**：新增 `MaxPointsPairwise = 10`（逐对/混合出行），驾车仍 50。
    限制必须按最坏路径算：逐对下 50 点 = 2450 次请求 ≈ 14 分钟
  - **`/route` 补 mode 校验**；`RoutePolyline` 对未知 mode 改为报错（原来 `default: return nil,nil`
    把"参数拼错"伪装成"正常返回空"），transit 仍显式返回 `(nil,nil)` 表示"业务上就没轨迹"
  - **消除重复的 haversine**：导出 `matrix.Haversine(a,b)`，删掉 api 里那份（原注释说"避免循环依赖"
    是错的——api 本来就 import 了 matrix）
  - **高德根地址从包级变量改成 `Client` 实例字段** + 路径常量化（`pathDistance`/`pathWalkingV5`…），
    新增 `NewClientWithBase` 供测试用。**教学点：包级可变状态 = 测试互相干扰 + 加 `t.Parallel()` 就 race**
  - **`Memory` 缓存补上过期清理**（`sweepThreshold=1024` 时写入前 sweep）：原来只在读同一 key 时才删
  - **前端 XSS**：新增 `esc()`，所有拼接外部文本的地方（搜索候选/地点名/警告）先转义
  - **缓存号漏 bump**：`app.js?v=20260827a` → `v=20260914a`（app.js 08-28 改过但没 bump）
  - `index.html` 内容更新（搜索框/城市框 placeholder 改成故宫/北京）；`.gitignore` 去掉改名前残留的 `/routeplanner`
- **前端默认视野改为北京**（`DEFAULT_CENTER=[116.397,39.909]`，原来是广州），
  并在 `initMap` 里留了 **GPS 定位的 TODO**（说明 `navigator.geolocation` 是异步、需 https、
  失败要安静退回默认中心）。搜索/城市输入框 placeholder 同步改成故宫/北京
- **新增 `internal/api/router_test.go`（11 个测试函数 + 2 个子测试）** ⭐ —— api 层第一次有测试：
  - 假高德按路径分发，`failPaths` 能**单独弄坏某一个接口**（`/v3/distance` 或 `/v5/direction/walking`），
    这是"测得出降级有没有上报"的前提
  - 覆盖：非法 mode/坐标/segments → 400；点数上限按 mode 分档（12 点步行 400、驾车 200）；
    TSP 起点不变量；**矩阵整体降级 → `is_degraded=true`**；**没配 key 不算降级**；
    混合出行只报坏掉那一段；`/route` 非法 mode 400、transit 空数组
  - 测试里刻意**用字面量写路径**而不是引用 amap 常量、**重写一份 `planRespJSON`** 而不是复用
    `planResp`——测的是 wire contract，引用内部定义就失去意义了
  - **验证过测试有牙齿**：临时把 `IsDegraded` 退回旧写法，`TestPlanReportsMatrixDegradation` 立刻红
- **`amap_test.go` 同步重构**：删掉改包级 `baseURL` + `defer` 还原的老套路，改用 `NewClientWithBase`；
  新增"搜索经纬度不能解反"和"RoutePolyline 未知 mode 必须报错且不发请求"两个用例
- **`planResp` 新增 `order_idx`**；README / `architecture-diagrams.md` / 设计文档全部同步
  - 设计文档文末新增**「附录：与实现的偏差」**（6 条：slog 没用上、**重试未实现**、表名 plans→tasks 等）
    + 「还没做但已知是债」（重试、缓存击穿、web 相对路径、XSS 彻底方案）
- **验证**：`go vet ./...` 干净；`go test ./...` 全绿（amap 0.47s、**api 0.65s**、solver）；
  `node --check web/app.js` 通过；`gofmt` 已格式化 `amap.go`/`router.go`
  （`internal/model/model.go` 与 `main.go` 仍不合规，未动）
- **⚠️ 遗留**：08-28 那轮工作（Mermaid 文档 / smoke.sh / 缓存错误处理）**至今未提交**，
  加上本次改动，`git status` 已积压 8 个改动文件 + 2 个未跟踪目录 —— **建议先 commit 再动 Phase 2**
- **后续可做**：①指数退避重试 ②缓存击穿（singleflight）③GPS 定位 ④`(0,0)` 坐标应视为"没传"
  ⑤cache/matrix 层补测试 ⑥`go:embed` 托管前端

### 2026-08-28（DSH 会话）— 项目现状梳理 + Mermaid 架构文档 + 环境就绪 + Phase 2 启动 ⏳

- **通读全项目代码**（main.go + internal 8 个包 + web 两个文件 + README），跑 `go vet ./...` + `go test ./...` 全过（internal/amap 1.0s、internal/solver 1.6s）
- **`docs/architecture-diagrams.md` 整篇重写为 Mermaid 版** ⭐（原为 718 行 ASCII 图，0 个 mermaid，且未纳入 git）：
  - 4 张图：系统架构（graph TB）、包依赖（graph LR + 依赖规则表）、`/plan` 数据流（flowchart，含两条分支）、一次规划时序图（sequenceDiagram，含缓存命中路径）
  - 另加：三层降级 flowchart、降级决策表、缓存 key 设计、API 速查表
  - **修正过时信息**：`internal/model/point.go` → `model.go`；技术栈拆成「已实现」/「规划中（Phase 2、3）」两张表（原文 MySQL/gRPC 混在一起易误读为已有）
  - 保留教学价值章节：架构设计七原则（DIP/SRP/防御性/Fail Open/可观测性/契约先行/前后端分离）+ 五个设计模式
- **修两个小坑**：
  - `.gitignore` 加 `/awesomeProject`（39MB 编译产物，之前 `git status` 里是 `??`，会被误提交）
  - `web/index.html` 删 `#modeGroup` 那 4 行死 CSS（全局统一方式按钮早已删除，样式残留）
- **新增 `scripts/smoke.sh` 真 key 冒烟测试** ⭐（教学点：单测用 httptest 抓不到真实 API 行为，外部集成必须真 key 冒烟一次）：
  - 8 组检查：服务/Redis 存活、清缓存、首次 `/plan` 真实路网、缓存写入+TLL、二次请求提速、`/search`、`/route` 轨迹点数、混合出行 walking/transit、参数校验 400
  - **判定降级的巧招**：3 固定点驾车真实路网 ≈ 45 km vs haversine ≈ 29 km，用区间断言 40~50 就能识破"接口 200 但其实降级了"
  - 无 key 模式下自测：6 通过 6 失败，失败项全部精确指向"未配置 AMAP_KEY"（证明断言有效）
- **接口实测（无 key 模式）**：`/healthz` 200、`/` 200（8887 字节，改动生效）、`/app.js` 200、`/vendor/sortable.min.js` 200、`POST /plan` 200 → 29.09 km（haversine）、`/search` 与 `/route` 503（符合设计）
- **环境重大更新** ⭐：
  - **Docker daemon 现在完全正常**（记忆里那条 Keychain -67674 坑未复现），`docker ps` 正常
  - **MySQL 8.4 镜像本机已有**，已起容器 `routeplanner-mysql`（3306，库 `routeplanner` 已建，版本 **8.4.11**）
  - ⚠️ 镜像为 `linux/amd64`、宿主 arm64 → 走 Rosetta 模拟，**能用但慢**
  - Redis 7.2.5 已在 6379 运行
- **Phase 2 概念已讲**（异步化动机 + Redis Stream + 表设计 + 状态机）：
  - **痛点算账**：步行/公交逐对 + 350ms 节流 → 3 点 2.1s、5 点 7s、**10 点 31.5s**；同步 HTTP 让浏览器转圈半分钟且易被网关掐断
  - Stream 相比 List 的三个关键能力：**消费组**（XREADGROUP 竞争消费，一条只给一个 worker）、**ACK+PEL**（worker 崩了任务不丢）、**XAUTOCLAIM**（接管超时未 ack 的任务）
  - `tasks` 表已设计好（id/status/req_json/result_json/error/created_at/updated_at + idx_status_created），状态机 pending→running→done/failed
  - 包规划：`internal/storage`（Repository 接口 + MySQL 实现）、`internal/queue`（Stream 生产消费）、`internal/worker`（消费逻辑）；`api` 只改两处（`POST /plan` 变投递 + 新增 `GET /plan/{task_id}`）
- **踩坑记录（写脚本时）**：① macOS 的 BSD `xargs` **不支持** GNU 的 `-r`；② `set -u` 下变量紧跟中文必须写 `${var}`，否则 bash 把中文当变量名一部分 → `unbound variable`（如 `$keys（` 要写 `${keys}（`）

### 2026-08-27（DSH 会话）— 安装技能 + 代码质量优化 ✅

- **新增技能/插件**：
  - **superpowers-dsh** ([GitHub](https://github.com/LayneChai/superpowers-dsh))：TDD、调试、规划和协作技能
  - **dsh-ponytail** ([GitHub](https://github.com/MengYuil/dsh-ponytail))：懒人资深开发模式，支持 `/ponytail-review/audit/debt/gain/help` 命令
  - **grimoire** ([ClaudSkills](https://claudskills.com/skills/grimoire/))：技能管理器（用户说的 "grim me"）
  - **安装方式**：`dsh plugin --profile web add <package>`（安装到 web profile）
  - **推荐工具**：[dsh-skill-station](https://github.com/WilShi/dsh-skill-station) - 技能站，可一键扫描导入各种技能
- **代码质量优化（后台分析报告）**：
  - ✅ **并发安全**：通过（Memory 缓存正确使用 sync.Mutex）
  - ✅ **资源泄漏**：通过（所有 HTTP 响应体都 defer 关闭）
  - ⚠️ **错误处理**：2 个中优先级问题（已修复）
  - ✅ **Panic 风险**：通过（所有数组访问有边界检查）
  - ℹ️ **代码重复**：3 处可优化（已修复 2 处）
- **Redis 错误处理优化**（`internal/cache/redis.go`）：
  - `Redis.Get`：区分 `redis.Nil`（正常未命中）和连接错误（记录日志）
  - `Redis.Set`：记录写入失败的错误日志（`log.Printf("[redis] set key=%s failed: %v")`）
  - **教学点**：缓存系统要容忍故障（fail open），但需要记录日志便于排查；日志格式统一：`[模块名] 操作 key=%s failed: %v`
- **抽取坐标解析函数**（`internal/amap/amap.go`）：
  - 新增 `parseCoord(s string) (lng, lat float64, err error)` 统一处理 "lng,lat" 字符串解析
  - `SearchPlaces` 使用新函数，消除重复代码（15 行 → 3 行）
  - **教学点**：重复代码超过 3 次就该抽取；函数返回多个值（Go 特色）；错误信息要具体
- **参数校验增强**（`internal/api/router.go`）：
  - 定义常量 `MaxPoints = 50`（最大点数限制，避免性能问题）
  - 添加 `isValidCoord(lat, lng float64) bool` 校验坐标范围（lat: -90~90, lng: -180~180）
  - 添加 `isValidMode(mode string) bool` 校验出行方式
  - `plan` 函数开头添加完整校验：点数超限、坐标有效性、segments 数组长度和内容合法性
  - **教学点**：HTTP 接口层要做参数校验（防御性编程）；常量定义在文件顶部便于修改；返回具体错误信息方便前端显示
- **部分失败容错 + 用户可见的降级提示** ⭐：
  - **后端响应结构扩展**：`planResp` 新增 `Warnings []string`、`Degraded []string`、`IsDegraded bool` 字段
  - **混合出行降级**：某段失败时，降级到 haversine 直线距离，记录日志和警告信息，继续计算其他段
  - **前端显示警告**：`plan()` 函数检测 `is_degraded` 标志，渲染黄色警告框（`.warning-box`）显示具体降级路段
  - **教学点**：部分失败容错比"要么全对要么全错"更友好；降级必须透明（用户需知道哪些是真的，哪些是降级的）；日志 vs 用户提示（详细错误信息记录日志，简明提示返回用户）
- **版本号**：前端 bump 到 `v=20260827a`；go vet/build/test 全过

### 2026-08-20（DSH 会话）— 修 Sortable 索引 bug + 本地化拖拽库 ✅

- **🐛 拖拽报错 `Cannot read properties of undefined (reading 'name')` 根因**：列表混着 li(地点)+div(段连接线)，Sortable 的 `oldIndex/newIndex` 是"所有子元素"索引（把 div 也算进去）→ splice 越界 → undefined 混进 state.points → renderList 崩溃。**修复**：改用 `oldDraggableIndex/newDraggableIndex`（只算可拖拽 li 的相对索引，正好对应 points 数组）；renderList 加 `if (!p) return` 健壮性
- **SortableJS 本地化**：浏览器 Tracking Prevention 拦 jsdelivr CDN → 下载到 `web/vendor/sortable.min.js`，script 引用本地路径
- 版本号 `v=20260820i`；vendor 200 验证过

### 2026-08-20（DSH 会话）— 前端 UI 优化：默认手动 + 段间选择 + 彩色图例 ✅

- **默认手动模式**：`manualCheck` 默认勾选；删掉全局统一出行方式按钮（modeGroup/currentMode）——所有请求走 segments；取消勾选=自动 TSP（统一驾车）
- **去掉经纬度显示**：renderList 不再显示坐标（用户觉得不重要）
- **段选择移到两点之间**：列表结构改为"地点 li / 段连接线 div / 地点 li / ..."，段按钮在连接线上（CSS ::before/::after 横线），Sortable `draggable: 'li'` 只拖地点，段线不参与；拖完 renderList 重排
- **路线半透明 + 图例**：每段画**独立 Polyline**（颜色=方式：驾车蓝 #1a73e8 / 步行绿 #34a853 / 公交橙 #f9a825），`strokeOpacity: 0.7`；`state.polyline` 改 `state.polylines` 数组；地图右上角图例（画完路线才显示）
- 版本号 `v=20260820h`；node check + 后端混合出行回归（40.81 km）全过

### 2026-08-20（DSH 会话）— 修复拖拽复制 bug + 手动设置 UI 优化 ✅

- **🐛 拖拽复制 bug（3→6）根因**：`redrawMarkers()` 为了重画地图标记调用了 `addPoint`，而 addPoint 会 `state.points.push`——拖拽 onEnd 在列表非空时调用 redrawMarkers，每个点被复制一遍。**修复**：redrawMarkers 直接 `new AMap.Marker` 不再调 addPoint（注释里明确警告）
- **🐛 Sortable 重建 DOM 后引用失效**：renderList 全量重建 li 后 Sortable 持有旧元素引用 → `initSortable` 改为可重入（先 destroy 再 new，onEnd 末尾重新绑定）
- **手动设置 UI**：每段方式按钮**始终显示**，未勾选"按列表顺序"时禁用（半透明 + 悬停提示"勾选后可为每段选择出行方式"），勾选后激活可点
- 版本号 `v=20260820g`；node check 过；后端混合出行验证正常（40.81 km）

### 2026-08-20（DSH 会话）— 混合出行(每段独立方式) ✅

- **需求**：每个路段单独选出行方式（如 步行→公交→驾车），而非全程统一
- **设计**：混合出行只在**手动模式**下生效（TSP 需要统一方式的全矩阵，每段不同=没有全局最优顺序可言）——勾选"按列表顺序"后，每个地点下面出现"到下一站"🚗🚶🚌 小按钮组
- **后端**：`/plan` 支持 `segments: ["walking","transit",...]`（长度=点数-1，否则 400）——逐段调 `amap.Distance(a,b,mode)`（新增导出：单对距离，缓存 key 带 mode + 350ms 节流）求和，顺序=列表顺序，跳过 TSP/全矩阵
- **前端**：`state.legs` 数组存每段方式；renderList 给每点（除最后）渲染 leg-modes 按钮组（手动勾选才显示）；拖拽重排后 legs 重置为 driving（简化）；drawRoute 手动模式逐段用对应方式调 /route
- **验证**：步行+公交混合 = 40.81 km（广州塔→白云山步行 + 白云山→长隆公交）；全驾车 45.44（TSP 重排）；segments 长度错误返回 400
- 版本号 `v=20260820f`；go vet/build/test + node check 全过

### 2026-08-20（DSH 会话）— 路线后端代理 + SortableJS + 分段按钮 ✅

- **问题**：①地图不显示驾车路线（AMap.Driving 走 JS key 通道，与 PlaceSearch 同病——JS key 无权限）；②手写 HTML5 DnD 体验差（元素被"复制"）；③原生 select 丑
- **路线走后端代理**：新增 `GET /route?origin=lng,lat&dest=..&mode=..` → `amap.RoutePolyline`：驾车 v3/direction/driving、步行 v3/direction/walking（**v5 步行不返回轨迹坐标**，实测确认）、公交返回空（结构复杂，前端画直线）；解析 steps[].polyline（"lng,lat;lng,lat"）拼全程轨迹；实测驾车 553 点/步行 689 点
- **拖拽换 SortableJS**（CDN 引入，`onEnd` 同步 state.points + renderList + redrawMarkers）；删掉手写 drag 事件
- **出行方式改分段按钮组**（🚗/🚶/🚌 三个按钮替代 select，`currentMode()` 读取）；交互设计教学点：选项少用按钮不用下拉
- 前端版本号 `v=20260820e`；后端 go vet/build 全过

### 2026-08-20（DSH 会话）— 出行方式 + 拖拽排序 ✅

- **多出行方式**：`model.Mode`（driving/walking/transit）+ `amap.DistanceMatrix(points, mode)`——驾车走 v3/distance 逐列批量（现有）；步行走 v5/direction/walking、公交走 v3/direction/transit/integrated（均逐对 + **350ms 节流**，实测高德个人 key 步行 QPS≈3/s，连发报 CUQPS_HAS_EXCEEDED_THE_LIMIT 导致矩阵降级 haversine）；**骑行对个人 key 不可用**（RESOURCE_UNAVAILABLE，暂不提供）
- **缓存 key 带 mode**：`cache.Cache` 接口改为 `Get/Set(key string)`，key 构造归 amap（`mode|coord->coord`）——同一点对驾车/步行距离不同，必须分开缓存
- **手动顺序**：`/plan` 支持 `manual:true`——跳过 TSP 按列表顺序算总距离（用户拖拽排序后想自己控制路线）
- **前端**：出行方式下拉（🚗/🚶/🚌）+ "按列表顺序"开关 + **列表 HTML5 拖拽排序**（原生 drag&drop，零依赖，重排后 redrawMarkers）；版本号 `v=20260820d`
- **实测对比**（广州塔/白云山/长隆）：驾车 45.44 / 步行 37.11 / 公交 40.97 km；手动顺序 56.76 vs TSP 45.44（**+25%，TSP 有效的活教材**）；二次请求全缓存命中、零降级
- 教学点：出行方式=不同 API 的抽象；逐对接口要节流尊重配额（批量接口的另一优势）；拖拽=用户干预 vs 算法优化

### 2026-08-20（DSH 会话）— 搜索支持全国 ✅

- 用户问"为什么默认广东"——之前 `/search` 把城市硬编码为广州。改为**城市可选参数**：`GET /search?q=xxx&city=可选`，city 空 = 全国搜索（不传 city/citylimit），有值 = 限定城市
- 前端搜索框下加城市输入框（留空=全国）；验证：全国搜"西湖"返回杭州西湖等、限定"杭州"更精准
- 教学点：参数化设计 + 地名歧义（精度 vs 覆盖度权衡）；版本号 bump 到 `v=20260820c`

### 2026-08-20（DSH 会话）— 搜索改为后端代理 ✅

- **问题**：前端 `AMap.PlaceSearch` 用 JS key 搜索返回"没有找到相关地点"——JS key（`<JS key>`）在搜索通道无权限/配额；**Web 服务 key（`<Web服务key>`）调 v3/place/text 完全正常**（curl 实测 6 结果）
- **方案（架构升级）**：搜索改走**后端代理**——新增 `GET /search?q=关键词`：后端用 Web 服务 key 调高德 v3/place/text（`amap.SearchPlaces`，城市写死广州，6 条/页），返回 `{places:[{name,address,lat,lng}]}`；前端 searchPlaces 改 `fetch('/search')`，**不再依赖 JS key/地图加载**
- **教学点**：API 代理模式——key 藏后端、可统一缓存/限流/日志；前端不直接持有第三方凭据
- **代码**：`internal/model` 加 `Place`；`internal/amap` 加 `SearchPlaces`；`internal/api/router.go` 加 `/search`（无 key 返回 503，Server 持有 amap 客户端，`NewRouter(m, am)`）；`main.go` 传 am；`web/app.js` 搜索改 fetch；`web/index.html` 加 favicon（消 404）+ 版本号 `v=20260820b`
- **验证**：`/search?q=广州塔` 返回候选（广州塔/地铁站等）；`/plan` 正常；go vet/build + node --check 全过

### 2026-08-19（DSH 会话）— 前端升级完成，待浏览器验证 ⏳

- **前端"搜索添加地点"完成**：`web/index.html` + `web/app.js` 增加 `AMap.PlaceSearch`（POI 搜索，限定广州，6 条/页）——输入地名→候选列表→点选自动带坐标添加；手动经纬度表单折叠为 `<details>` 高级选项
- **真实路网画线完成**：`drawRoute` 改为 `AMap.Driving` 逐段驾车规划（order[i]→order[i+1]）拼接真实轨迹，相邻段去重衔接；**单段失败降级画直线**（和后端降级同思想）；插件按需加载（AMap.plugin）；回调包 Promise
- **设置面板加"安全密钥(可选)"**：`localStorage('amap_jscode')`，开了安全密钥才需要填；`window._AMapSecurityConfig` 在加载脚本前设置
- **⏳ 未验证**：用户浏览器还没实测（JS key `<JS key>` 渲染、搜索、画线）。服务已全部关闭
- 环境备忘：JS key 可能需要控制台配域名白名单（localhost 留空即可）；`node --check web/app.js` 已过，后端 /plan 正常（45.44 km）

### 2026-08-19（DSH 会话）— Redis 缓存完成 ✅

- **新增 `internal/cache/redis.go`**：`Redis` 实现 `Cache` 接口（go-redis/v9，Ping 构造时验证连通性，`dist:` key 前缀，Set 自带 TTL）。**`amap` 客户端零改动**——上一步定义接口的意义兑现
- **`main.go` 两级降级**：缓存层 Redis 优先→连不上降级内存 map；距离层高德→haversine。每级降级都打日志
- **环境坑（重要）**：Docker 拉镜像报 Keychain 错误 -67674（headless 无法读 macOS 钥匙串，干净 config 也无效，daemon 侧问题）；brew 安装被沙箱权限拦（~/Library/Caches 在工作区外 + Cellar 需 sudo）。**最终方案：源码编译 Redis 到 `.redis-src/redis-7.2.5/`**（已在 .gitignore）；redis 二进制在 `.redis-src/redis-7.2.5/src/redis-server`，redis-cli 同目录。Go 工具链沙箱处理：`GOPATH/GOCACHE/GOMODCACHE` 指工作区（.gopath/.gocache/.gomodcache），**每次 go 命令都要带这三个 env**
- **验证结果**：Redis 6 个缓存 key（3 点 6 个有向对）、TTL≈24h（86397s）、二次 /plan 零 amap 请求、**重启 Go 服务后仍零请求（缓存跨进程存活）**
- **环境变量**：`AMAP_KEY=<Web服务key>`（Web 服务 key）启动即走真实路网 + Redis 缓存

### 2026-08-19（DSH 会话）— 高德真实验证完成 ✅

- **用户已申请高德 key 并验证通过**：两个 key 中 `<Web服务key>` 才是 **Web 服务 key**（`<JS key>` 是 JS API key，用户最初报反了；`USERKEY_PLAT_NOMATCH` 即平台类型不匹配）。配置：`export AMAP_KEY=<Web服务key>` 重启服务。⚠️ **不要把 key 值写进会提交 git 的文件**
- **重要发现（实测高德）**：v3/distance 批量语义是**"多起点 → 单终点"**，不支持"多对多一一对应"（返回 INVALID_PARAMS，文档写错了）。矩阵构建改为**逐列批量**：一次请求 = 所有点 i → 点 j，n 点 n 次请求
- **真实验证结果**：广州塔/白云山/长隆 3 点，高德路网 45.44 km vs haversine 29.21 km（+55%），且 TSP 最优顺序改变（数据变了结论就变）；`[amap]` 请求日志 3 条/次；**缓存生效：第二次 /plan 零请求、0.33s→0.06s**
- **教训**：单测用假服务器抓不到"批量语义"这类真实 API 行为，外部 API 集成必须做一次真 key 冒烟测试；降级机制让批量失败时服务照常跑
- 之前代码（同一天）：internal/cache、internal/amap、matrix 降级、main 装配、amap_test 4 用例

### 2026-08-16（Claude 会话，历史）

- 当前阶段：**Phase 0 完成；Phase 1 MVP 起步——前端已先行，TSP 已深化**
- 已教内容：
  - 概念：net/http vs gin；go mod tidy 自动拉包 / go get；高内聚低耦合 + internal 强制封装；**从需求出发、从前端倒退需求**（契约先行）；gin NoRoute 静态托管 + httprouter 不允许根 catch-all 与具体路由共存这个坑
  - 项目结构：`internal/{config, model, matrix, solver, api}`，按 Phase 3 gRPC 边界拆包
  - 后端代码：gin 路由（/healthz + POST /plan）；POST /plan = haversine 矩阵 → 最近邻 TSP → 顺序+总距离；solver 有单测
  - 前端代码：`web/{index.html, app.js}`——地图点选 + 手动加坐标、调 /plan、画折线、设置面板换 key
  - 验证：go vet/build/test 全过；服务跑通，GET /、/app.js、/healthz、POST /plan 全 200
- **TSP 进展**：solver 现在是 最近邻 → 2-opt → 模拟退火 三级。核心思想：2-opt 反转中间段消除交叉（只接受变好，到局部最优）；模拟退火按 exp(-Δ/temp) 概率接受差解跳出局部最优，温度指数降。对比测试（seed=5，12 点）实证 322.44 → 289.14 → 285.62 km 逐步变好。`TourLength` 已导出供 api 复用。
- 上次动手产物：`web/index.html` + `web/app.js`；`internal/solver/tsp.go` 新增 TwoOpt/SimulatedAnnealing/TourLength；`api/router.go` 改 NoRoute 托管 + /plan 换用模拟退火；config 端口 7800

## 下次会话从这里继续

1. **状态（2026-09-14）**：Phase 1 功能全部完成；**前端已重构为 React 并成为唯一前端**（挂站点根路径），
   手写版已删；本轮做了 14 项浏览器实测（见进度追踪）。**只差最后一步收尾：带 AMAP_KEY 的真实路网 + Redis 缓存实测** ⏳
   - 🔑 **`.env` 已建好并写入用户那把 key**（2026-09-14 找到：存在**他日常用的 Edge** 的
     localStorage 里，键名 `amap_key`，值 `2d47bee5c94656cd615a644a271b2c61`）
   - ⚠️ **但后端暂时还用不了**：该 key 只绑定了「Web端(JS API)」平台，
     实测打 REST 接口返回 `USERKEY_PLAT_NOMATCH`(10009) —— key 本身有效，是平台不对
     （对照：编造的 key 返回 `INVALID_USER_KEY` 10001）
   - **待办（等用户去控制台点两下）**：① 在 console.amap.com 给这个 key **追加勾选「Web服务」平台**
     （不用重新申请，key 值不变 → `.env` 无需改）；② 顺手复制「安全密钥」备用
     （只有将来要做"前端直连搜索"才需要，地图渲染不需要它）。
     **用户做完回来说一声** → 停旧实例 → `set -a; source .env; set +a; ./awesomeProject &` → `bash scripts/smoke.sh`
   - ⚠️ 用户已明确选择路线：**保持后端代理架构**（不走前端直连搜索），所以本轮只订正了文案，没动搜索实现
   - 期望结果：**12/12 通过**，首次 `/plan` total_km ≈ 45（真实路网），二次请求 0 个 `[amap]` 日志
   - 配了 key 之后**才能测的前端部分**（本轮无 key，这 6 项没覆盖）：地图渲染 / 地图点选 /
     搜索候选列表 / 真实路网 polyline / 降级警告 / 图例与缩放
2. **Phase 2 已启动**（环境全部就绪，概念已讲）：Redis 7.2.5（6379）+ MySQL **8.4.11** 容器 `routeplanner-mysql`（3306，库 `routeplanner` 已建）
   - **下一步第一步**：建 `tasks` 表 + `internal/storage` 包（Repository 接口 + MySQL 实现）——教学点：`database/sql`、连接池参数、DSN、MySQL 8 的 `caching_sha2_password`
   - 后续顺序：`internal/queue`（XADD / XREADGROUP / XACK / XAUTOCLAIM）→ `internal/worker` 消费逻辑 → `api` 改造（`POST /plan` 变投递返回 202 + task_id，新增 `GET /plan/{task_id}` 查询）→ 前端轮询显示进度
3. **可选项**：① 前端搜索框防抖（低优先级）；② 模拟退火固定随机种子；
   ③ **给 `frontend/` 加测试** —— 目前前端零测试，`go test` 管不到它。可上 Vitest，
      优先覆盖 `usePoints.movePoint`（重排 + legMode 重置）和 `api.js` 的错误归一化；
   ④ 深浅色主题（`styles/tokens.css` 已经把设计变量抽出来了，具备换主题的条件）
4. 启动命令（**已修正**）：
   - Redis：`.redis-src/redis-7.2.5/src/redis-server --port 6379 --save '' --appendonly no &`
   - MySQL：`docker start routeplanner-mysql`（首次创建：`docker run -d --name routeplanner-mysql -e MYSQL_ROOT_PASSWORD=root123 -e MYSQL_DATABASE=routeplanner -p 3306:3306 mysql:8.4`）
   - 后端：`go build -o awesomeProject .` 后 `set -a; source .env; set +a; ./awesomeProject` —— **二进制名是 `awesomeProject`，不是 `routeplanner`**（README 用 `go run .` 也可以）
5. 备忘：
   - Go 命令要带 `GOPATH/GOCACHE/GOMODCACHE` 三个 env（指向工作区）
   - **前端改动流程**：`cd frontend && npm run build`（产物落到 `../web/`）。
     **不再需要手动 bump `app.js?v=`** —— Vite 产物文件名自带内容 hash（如 `index-CFMrierg.js`），
     代码一改 hash 就变，浏览器缓存自然失效。手动 bump 是旧手写版才需要的
   - **Redis 没有丢**：二进制在 `.redis-src/redis-7.2.5/src/redis-server`（源码编译版，
     `.redis-src/` 已被 gitignore），只是当前没在跑。要用就先启动它
     （⚠️ 无 key 时 Redis 根本不会被装配，见 `main.go`）
   - shell 脚本坑：`${var}` 必须加花括号（紧跟中文会被 bash 当变量名的一部分）；
     macOS BSD `xargs` **无 `-r`**；BSD `grep` **不支持 `\|`**（要用 `grep -E`）；
     BSD `sed -i` 需要跟一个后缀参数
   - DSH 已安装 superpowers/ponytail/grimoire 技能；MySQL 镜像是 linux/amd64 → arm64 宿主走 Rosetta 模拟（慢但可用）

---

**维护规则**：每次教完一个知识点，更新"进度追踪/下次继续"。更新本文件时，同步考虑是否回写 Claude 记忆 `~/.claude/projects/-Users-shoushenwei-GolandProjects-awesomeProject/memory/teaching-plan.md`，保持两套记忆一致。
