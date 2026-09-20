<div align="center">

# 🛣️ Route66

**多点路线智能规划器 · 手写 TSP · 多出行方式 · 极客教学项目**

`Go` · `Gin` · `高德地图` · `手写TSP` · `Redis` · `React 19` · `手写 ReAct Agent`

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev/)
[![Gin](https://img.shields.io/badge/Gin-1.12-008ECF?style=for-the-badge&logo=gin&logoColor=white)](https://gin-gonic.com/)
[![高德](https://img.shields.io/badge/高德地图-API-red?style=for-the-badge&logo=amazonaws&logoColor=white)](https://lbs.amap.com/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=for-the-badge&logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)](LICENSE)

> 一条路线，三种走法，给你最优解。
> 从**最近邻 → 2-opt → 模拟退火**，亲手写一个旅行商问题求解器。

</div>

---

## 🔥 这是什么？

**Route66** 是一个用 Go 手写的**多点路径规划&优化器**。输入一串地点，它能帮你算出**按照访问顺序怎么走最省**（TSP 旅行商问题），并且支持**驾车 / 步行 / 公交**三种出行方式、甚至**每段路用不同方式**（混合出行）。

但它的灵魂不只是"能用"——这是一个**教学项目**：每个模块都像一个拆开的积木，配套中文注释讲透了「为什么」。从 `net/http` 到 `Gin`，从手写 TSP 到 Redis 缓存，一步一个脚印。

一个很 geek 的名字：**Route 66**，美国传奇的「母亲之路」，横贯东西。让它带你的路线「上路」。

## ✨ 功能特性

| 特性 | 说明 | 亮点 |
|---|---|---|
| 🎯 **TSP 路径优化** | 手写求解器 | 最近邻 → 2-opt → 模拟退火，一步步看解变好 |
| 🚗🚶🚌 **多出行方式** | 驾车 / 步行 / 公交 | 每种方式走不同高德接口，实测距离差异 |
| 🔀 **混合出行** | 每段路独立选方式 | 步行→公交→驾车，按你的方式导航 |
| 🖱️ **拖拽排序** | 原生 HTML5 DnD，零依赖 | 想手动控制顺序就手动，想优化就交给算法 |
| ⚛️ **React 前端** | React 19 + Vite | `useState` 描述状态，不再手写 DOM 操作 |
| 🗺️ **地图点选** | 高德 JS API | 点地图即加地点，搜索地名自动带坐标 |
| 🎨 **彩色路线 + 图例** | 每段一种颜色 | 颜色即出行方式，一眼看懂 |
| 🤖 **智能 Agent** | 手写 ReAct 循环，零框架 | 模型自主组合「搜地点 → 规路线」两个工具，全过程可观察 |
| ⚡ **双重缓存** | Redis + 内存 | 缓存命中零外部请求，重启跨进程存活 |
| 🛡️ **优雅降级** | 高德↔haversine | 外部 API 挂了服务照跑，矩阵不混搭 |

## 📸 效果预览

```
┌─────────────────────────┬──────────────────────────────────┐
│  🔍 搜索添加地点          │   🗺️  高德地图                    │
│  [故宫        ] [搜索]     │        ·· 起点                    │
│  [城市(可选)          ]   │     🟦━━━━━┓                      │
│  ───────────────         │     🟩━━━━▓▓▓  (步行)             │
│  ✓ 起点  天安门          │     🟧━━━━━━━┛  (公交)             │
│    ──🚗🚶🚌──            │                                │
│  ✓ 第1站  故宫            │   [图例] 🟦驾车 🟩步行 🟧公交       │
│    ──🚗🚶🚌──            │                                │
│  ✓ 第2站  天坛            │                                │
│  ☐ 手动设置每段出行方式✓    │                                │
│  [🚀 开始规划]            │                                │
└─────────────────────────┴──────────────────────────────────┘
```

## 🏗️ 架构

```
                    ┌──────────────────────────────┐
  浏览器(前端)        │        Go 后端 (Gin)          │
  ┌─────────┐       │  ┌────────────────────────┐  │      ┌────────────┐
  │ React    │──────▶│  │  /plan  /search /route │  │─────▶│  高德 Web   │
  │ 高德JSAPI│       │  │   (API 代理层)          │  │      │  服务 API   │
  │ Vite 产物│       │  └──────────┬─────────────┘  │      └────────────┘
  └─────────┘       │             │                │
                    │  ┌──────────▼─────────────┐  │      ┌────────────┐
                    │  │  matrix (距离矩阵)       │  │      │ Redis/内存  │
                    │  │  solver (手写 TSP)       │  │◀────▶│  距离缓存   │
                    │  └────────────────────────┘  │      └────────────┘
                    └──────────────────────────────┘
```

**分层设计**：`api`(编排) / `matrix`(距离来源) / `solver`(TSP) / `amap`(高德客户端) / `cache`(缓存抽象)——**低耦合，面向接口**。距离算错、算法换掉、缓存换 Redis，全都不用改调用方。

**包依赖关系**（单向无环，`internal/` 强制封装）：

```mermaid
graph LR
    MAIN["main.go<br/>装配层"]
    API["internal/api"]
    MX["internal/matrix"]
    AM["internal/amap"]
    SOL["internal/solver"]
    CA["internal/cache"]
    MOD["internal/model"]
    CFG["internal/config"]

    MAIN --> CFG
    MAIN --> CA
    MAIN --> AM
    MAIN --> MX
    MAIN --> API
    API --> MX
    API --> AM
    API --> SOL
    API --> MOD
    MX --> AM
    MX --> MOD
    AM --> CA
    AM --> MOD
```

| 依赖规则 | 原因 |
|---|---|
| 依赖方向**单向**，无环 | 环会让包无法单独测试、无法单独演进成微服务 |
| `solver` 只吃 `[][]float64`，**不认识 amap** | 算法不该关心距离从哪来；将来换 gRPC 距离服务，solver 零改动 |
| `api` 不直接算距离/排序 | 编排层只做参数校验与串联，业务在各自包里 |
| `amap` 依赖 `Cache` **接口**，不认识 Redis | 依赖倒置：换缓存实现，`amap` 零改动 |
| `matrix` 是 `amap` 与 `solver` 之间的**中间层** | 降级策略集中在一处，调用方无感知 |

## 🧠 为什么手写 TSP？（而不是调库）

因为**过程比结果更重要**。手写让你看到：

```
最近邻起步     →  322.44 km   (贪心，快但不最优)
+ 2-opt       →  289.14 km   (反转中间段消除交叉，到局部最优)
+ 模拟退火     →  285.62 km   (按 exp(-Δ/temp) 概率接受差解，跳出坑)
```

**测量 · 2-opt · 模拟退火**三级跳，每一步都看得见解在变好。这就是教学项目的意义——**亲手造轮子，才知道轮子为什么长这样。**

## 🚀 快速开始

### 前置条件

- **Go 1.26+**
- **(可选) Redis 7** —— 不装也能跑，自动降级内存缓存
- **(可选) Node 20+** —— 只在你要构建/改前端时需要（源码在 `frontend/`）

### 1. 申请高德 key（免费）

高德开放平台 [console.amap.com](https://console.amap.com) → 实名认证 → 创建应用。

**高德是按「服务平台」分权的，不是按 key 分的**——所以一个应用下**只申请一把 key，
在「服务平台」里把两个都勾上**就够了，不必申请两把：

| 平台 | 用途 | 配在哪 |
|---|---|---|
| **Web服务** | 后端算距离 / 搜地名 / 取路网轨迹（REST） | 服务端环境变量 `AMAP_KEY` |
| **Web端(JS API)** | 浏览器渲染地图（+ 可选的前端直连搜索） | 网页右上角「设置」里填 |

> 分成两把 key 也完全可以，只是要分别配到上面两个地方。**能省就省：一把 + 两个平台。**

**JS key 还有个必填项：安全密钥。** 2021-12-02 之后申请的 key，JS API 2.0 强制要求在
「设置」里一并填上控制台给出的那串安全密钥。**不填的现象很有欺骗性：地图能正常显示，
但搜索、算距离这类能力全部失败**——因为纯渲染地图不需要它，而要调接口的能力都需要。

**报错码 → 病因**（配错了直接对着这张表查）：

| 高德返回 | 含义 | 怎么办 |
|---|---|---|
| `INVALID_USER_KEY` (10001) | 高德不认识这把 key | key 抄错了，或已被删除 |
| `USERKEY_PLAT_NOMATCH` (10009) | key 有效，但没绑定你要的平台 | 去控制台给这把 key **追加**对应平台 |
| `INVALID_USER_SCODE` | 缺（或错）安全密钥 | 在网页「设置」里把安全密钥填上 |

### 2. 启动

```bash
# 起 Redis(可选,没有就自动用内存缓存)
redis-server --port 6379 &

# 起后端
export AMAP_KEY=你的Web服务key
go run .

# 打开页面
open http://localhost:7800
```

打开页面 → 右上角「设置」→ 填 JS API key（有安全密钥就一起填）→ 保存重载。

> JS key 现在有三层来源：**设置弹层的个人覆盖 > 管理台配置（DB）> env 兜底**。
> 后端的「Web服务」key 也支持管理台在线更换，**无需重启**（见下一节）。

### 2.5 登录系统 + 管理台（PostgreSQL，可选）

配了数据库后，服务多出认证与管理能力；**不配则全部不启用，同步规划照常**。

```bash
# 起 PostgreSQL(自建,推荐 docker compose)
docker compose up -d          # postgres:16-alpine,5432 端口

# .env 里追加(完整示例见 .env 本地文件,已 gitignore):
# PG_DSN=postgres://routeplanner:routeplanner@localhost:5432/routeplanner?sslmode=disable
# ADMIN_PORT=7801
# ADMIN_USER=admin
# ADMIN_PASSWORD=改成你自己的

go run .
```

| 端口 | 内容 |
|---|---|
| `:7800` | 用户端（现有规划界面 + `/auth/*` 登录注册 + `/config/public` 下发 JS key） |
| `:7801` | 管理台（掩码查看/在线更换高德 key、用户列表；非 admin 一律 403） |

- **key 优先级**：管理台(DB) > `.env`(兜底)。管理台改 key **立即生效不用重启**；
  把某项清空则回落到 env 兜底值
- **安全模型**：REST key 只存在服务端绝不下发；JS key 天生公开（下发不泄密）；
  密码 bcrypt 存储；登录失败不区分"无此用户/密码错"（防枚举）；
  会话为 HttpOnly cookie —— cookie 不按端口隔离，故 7801 每条路由都强制 `RequireAdmin`
- 管理台是原生 HTML 单页（`admin/`，不进构建），设计原则："不是所有前端都该上框架"

> ⚠️ 换库说明：2026-09-17 起从 MySQL 迁到 PostgreSQL（GORM driver 替换，
> repo 层业务代码零改动）；旧 `routeplanner-mysql` 容器可停用退役。

### 3. 用起来

1. **搜索地名**（如"广州塔"）→ 点选添加
2. 或者直接在**地图上点**加地点
3. **拖拽**调整顺序（想手动控制就勾选继续）
4. 每段路上选 🚗🚶🚌 出行方式
5. **🚀 开始规划** → 看彩色路线 + 总距离

### 4. 前端（React 19 + Vite）

`frontend/` 是 React 源码，构建产物直接输出到 `web/`，由 Go 托管在**站点根路径**。
产物不进仓库（`.gitignore` 掉了整个 `web/`），所以 clone 后要先构建一次：

```bash
cd frontend
npm install
npm run build      # 产物输出到 ../web/（index.html + assets/）
```

改前端时别每次构建，用 Vite 的开发服务器，带热更新（改完代码浏览器立刻变）：

```bash
cd frontend
npm run dev        # 打开 http://localhost:5173
```

> 开发服务器已经把 `/plan` `/search` `/route` 代理到 `localhost:7800`，
> 所以后端还是照常 `go run .` 起在 7800，两个进程同时跑。

**为什么产物丢进 `web/` 就能跑**：`router.go` 里有一句 `r.NoRoute(http.FileServer(http.Dir("web")))`，
它托管的是整个 `web/` 目录 —— 产物落在那里，前端就自动出现在根路径，**Go 代码一行都不用改**。

> ⚠️ `vite.config.js` 里的 `base` 和 `outDir` 是**一对**，必须指向同一个位置：
> `base` 决定 HTML 里资源路径怎么写（`/assets/...`），`outDir` 决定文件实际放哪。
> 只改一个就会出现"页面能开、JS 全 404"。

> **这段前端是从手写原生 JS 重写过来的。** 原来那一版（`web/app.js` + `web/index.html`，
> 用 SortableJS 拖拽）已经删除，但它的教训留在了 `AGENTS.md`：
> `renderList()` 全量重建 `<li>` 会让拖拽库持有的元素引用失效（必须 `destroy()` 再 `new`）；
> 删点要同时 `splice` `points` 和 `legs` 两个平行数组，忘一个就错位。
> 这两类 bug 正是 React 用 `key` 和单一数据源解决的。

## 📡 API

### `POST /plan` —— 规划路线

```bash
curl -X POST http://localhost:7800/plan \
  -H 'Content-Type: application/json' \
  -d '{
    "origin": {"name":"天安门","lat":39.9087,"lng":116.3975},
    "destinations": [
      {"name":"故宫","lat":39.9163,"lng":116.3972},
      {"name":"天坛","lat":39.8822,"lng":116.4066}
    ],
    "mode": "driving",                 // driving | walking | transit(非法值 → 400)
    "segments": ["walking","transit"]  // 混合出行:每段一种方式
  }'
```

```json
{"order": ["天安门","故宫","天坛"],      // 给人看的:名字
 "order_idx": [0,1,2],                  // 给机器用的:下标(画线按下标取点)
 "total_km": 6.8,
 "warnings": [],
 "degraded": [],
 "is_degraded": false}
```

> 为什么 `order` 和 `order_idx` 都要给:**名字不是标识符**。用户搜两次"故宫"就会有
> 两个同名点,前端拿名字回查坐标只能查到一个,线就画错了。名字负责人看,下标负责机器用。

| 参数 | 说明 |
|---|---|
| `origin` / `destinations` | 起点 + 目的地点列表 |
| `mode` | 全局出行方式（默认 `driving`）；**白名单校验**，非法值返回 400 |
| `segments` | 混合出行，`segments[i]` = 第 i 段方式（长度 = 点数-1）|
| `manual` | `true` 按列表顺序，跳过 TSP |

**点数上限按出行方式分档**（理由见 `internal/api/router.go` 里的算账注释）：

| 路径 | 上限 | 为什么 |
|---|---|---|
| 单一驾车 | 50 | 走 `v3/distance` 批量接口，n 个点只要 n 次请求 |
| 步行 / 公交 / 混合出行 | **10** | 逐对请求 + 350ms 节流，10 点约 32 s 已经是能忍的极限 |

**降级会显式回传**：`is_degraded=true` + `warnings` 说明哪些数字是直线估算。
（高德整体挂掉 → 一条覆盖全线的警告；混合出行某段挂掉 → 只报那一段。）

### `GET /search?q=关键词&city=可选` —— 地名搜索

```bash
curl "http://localhost:7800/search?q=故宫&city=北京"
```
```json
{"places":[{"name":"故宫博物院","address":"景山前街4号","lat":39.9163,"lng":116.3972}]}
```

### `GET /route?origin=lng,lat&dest=lng,lat&mode=driving` —— 真实路网轨迹

注意 `origin` / `dest` 是 **`经度,纬度`**（和高德一致，经度在前）。

```bash
curl "http://localhost:7800/route?origin=116.3975,39.9087&dest=116.3972,39.9163&mode=driving"
```

返回一串 `[lng,lat]` 坐标，前端拼成折线。`mode` 非法 → 400；`mode=transit` 返回空数组
（公交由"步行段+公交段"拼成，轨迹天然不连续 → 前端画直线，这是业务事实不是错误）。

## 🤖 智能 Agent（手写 ReAct，零框架）

`internal/agent/` 是一个**手写的最小智能体**：不依赖任何 Agent 框架，核心循环只有
"调模型 → 执行模型请求的工具 → 结果回填历史 → 循环"四步，直到模型返回纯文本即最终答案。
手写一遍是为了看清控制流全貌；后续引入 Eino 等框架时可作对照实现。

### 组成

| 文件 | 角色 | 说明 |
|---|---|---|
| `internal/agent/types.go` | 类型 | `Message` / `ToolCall` / `ToolSpec`，对应 OpenAI Chat 协议的消息形状 |
| `internal/agent/model.go` | 大脑 | `Model` 接口 + `OpenAICompatible` 实现（DeepSeek / Kimi / Qwen 兼容模式 / Ollama 通吃，换厂商只改 BaseURL + Model） |
| `internal/agent/tool.go` | 手 | `Tool` 接口：`Spec()` 给模型看，`Run()` 真执行 |
| `internal/agent/loop.go` | 躯干 | `Agent.Run()`：ReAct 循环 + 防失控（`MaxIterations`）+ 逐轮回调（`OnStep`） |
| `internal/agent/tools_plan.go` | 业务工具 | `search_place`（直通 `amap.Client.SearchPlaces`）、`plan_route`（直通 `planner.Compute`） |
| `cmd/agent-demo/main.go` | 装配层 | 独立入口，与 7800 服务互不依赖，跑完即退出 |

### 调用时序

```mermaid
sequenceDiagram
    autonumber
    participant M as main.go<br/>cmd/agent-demo
    participant L as Agent.Run<br/>internal/agent/loop.go
    participant MD as Model.Complete<br/>model.go
    participant T as Tool.Run<br/>tools_plan.go
    participant B as 业务包<br/>amap / planner

    M->>L: Run(ctx, question)
    loop 每一轮（上限 MaxIterations，默认 10）
        L->>MD: Complete(ctx, msgs, specs)
        MD-->>L: Completion{Text} 或 {Calls}
        alt 有 tool_calls
            L->>T: execTool → Run(json.RawMessage)
            T->>B: SearchPlaces / ParseMode + Compute
            B-->>T: []model.Place / planner.Result
            T-->>L: JSON 文本（错误也以文本返回）
            L->>L: NewToolMsg 回填历史，进入下一轮
        else 纯文本 = 最终答案（出口）
            L-->>M: answer, []Step, nil
        end
    end
```

### 一次真实问题的三轮走位

以"从天安门出发，途经故宫再到天坛的驾车路线"为例：

1. **第 1 轮**：模型发现"有地名没坐标"，发起 `search_place({"keyword":"天安门"})` → 高德返回带 `lat/lng` 的候选
2. **第 2 轮**：模型凑齐三个点，发起 `plan_route({points:[...], mode:"driving"})` → 直通 `planner.Compute`（模拟退火解序），返回 `order` / `total_km` / 降级警告
3. **第 3 轮**：信息足够，模型返回中文答案，循环结束

每一轮的"模型说了什么、调了什么工具、返回了什么"都会通过 `OnStep` 回调实时打印——跑一遍就能亲眼看到 agent 怎么思考。

### 关键设计

- **循环不做决策**：智能全在模型的每轮输出里，`loop.go` 只执行决策、管理历史、防失控
- **工具层刻意薄**：校验/解算/降级全部复用 `planner.Compute`，与同步接口、异步 worker 跑同一条解算链，零复制
- **失败喂回而非中断**：未知工具、非法参数、上游超时都转成"错误： …"文本喂回模型自行纠正；只有超过 `MaxIterations` 轮还在点工具才强制刹车
- **多工具并发执行**：模型一次点 n 个互不依赖的调用时，goroutine 并发执行，结果按下标对位回填
- **可测试性来自接口**：循环只依赖 `Model` / `Tool` 两个小接口，`fakeModel` + `echoTool` 注入即可零成本钉死循环逻辑（`-race` 下 4 个场景全绿）

### 怎么跑

```bash
set -a; source .env; set +a          # 复用 AMAP_KEY
export LLM_BASE_URL=https://api.deepseek.com   # 任选 OpenAI 兼容服务
export LLM_API_KEY=sk-xxx
export LLM_MODEL=deepseek-chat
go run ./cmd/agent-demo "帮我规划一条从天安门出发,途经故宫再到天坛的驾车路线"
```

> 未配置 `LLM_API_KEY` 时会提示后退出；未配置 `AMAP_KEY` 时规划工具走 haversine，
> agent 会如实告知是直线估算。当前状态：循环已打通并用假模型测试覆盖，接上真 key 即可实测。

## ⚙️ 运行时：启动与请求流转

### 启动时序（`main.go` 是装配层）

```mermaid
sequenceDiagram
    autonumber
    participant M as main.go<br/>func main()
    participant C as config.Load()
    participant CA as cache 包
    participant DB as PostgreSQL
    participant AM as amap.NewClient
    participant MX as matrix.Service
    participant API as api.NewRouter

    M->>C: Load()
    C-->>M: Config{Port, AMAP_KEY, RedisAddr, PG_DSN...}
    M->>CA: NewRedis(addr, 24h)
    alt Ping 成功
        CA-->>M: *Redis（跨重启存活）
    else 连不上（不报错不退出）
        CA-->>M: err → 降级 NewMemory(24h)
    end
    alt PG_DSN 已配置
        M->>DB: repo.NewGorm + Migrate
        Note over DB: tasks / users / app_settings 表就绪<br/>启用认证、异步任务、管理台(7801)
    else 未配置
        Note over M: 认证/异步/管理台全部不启用<br/>同步 /plan 照常
    end
    M->>AM: NewClient(key, distCache).WithKeyFn(动态取 key)
    Note over AM: key 每次请求现查（DB > env）<br/>管理台改 key 不重启即生效
    M->>MX: NewWithAmap(amapClient)
    M->>API: NewRouter(...) + NoRoute 托管 web/
    M->>M: router.Run(":7800")
    Note over M: 阻塞在 net/http Serve() 的<br/>for { Accept(); go c.serve() } 循环里
```

要点：

- **Redis 连不上不报错、不退出**——降级进程内 map，任何一个外部依赖挂掉服务照常跑
- 装配出的 `matrixService` / `amapClient` 是**全项目唯一实例**，所有请求共享——这也是缓存能跨请求命中的前提
- `router.Run()` 阻塞在标准库 `net/http.Serve()` 的 Accept 循环里：main 的代码停在这一行，但进程活着；每个连接一个新 goroutine，与 main 之间没有调用关系

### 一次 `/plan` 请求的时序

```mermaid
sequenceDiagram
    autonumber
    participant FE as 浏览器（React web/）
    participant P as router.go<br/>(s *Server) plan
    participant PC as planner.Compute
    participant MX as matrix.Service
    participant AM as amap.Client
    participant RD as Redis
    participant SV as solver

    FE->>P: POST /plan
    P->>P: ShouldBindJSON + ParseMode + Validate<br/>（校验收口在 planner 包，非法一律 400）
    P->>PC: Compute(points, mode, manual, segments)
    rect rgb(240, 240, 240)
        Note over PC,SV: 驾车分支
        PC->>MX: DistanceMatrix(points, mode)
        MX->>AM: drivingMatrix(points)
        loop 每一列 j（goroutine 并发，信号量限 4 路）
            AM->>RD: GET dist:driving|lng,lat->lng,lat
            alt 缓存命中
                RD-->>AM: km（零外部请求）
            else 未命中
                AM->>AM: GET v3/distance（5s 超时）→ SET 缓存
            end
        end
        PC->>SV: SimulatedAnnealing(dists, 0)
        Note over SV: 最近邻粗解 → 2-opt → 模拟退火
        SV-->>PC: order []int
    end
    alt 高德失败（任一列）
        MX-->>PC: haversineMatrix, degraded=true
        Note over PC: warnings 追加全线一条警告<br/>is_degraded = len(warnings) > 0
    end
    PC-->>P: planner.Result{order, order_idx, total_km, warnings}
    P-->>FE: 200 JSON（is_degraded=true 时前端显示警告条）
```

**并发含义**：主循环派发完 goroutine 立刻回到 `Accept` 接下一位，两个 `/plan` 可以同时在算。

**`/plan` 内部调用链**（带完整签名）：

```
(s *Server) plan(c *gin.Context)                                     api/router.go
├─ planner.ParseMode(s string) → (model.Mode, error)                 mode 白名单，空 = 默认 driving
├─ planner.Validate(points, mode, segments) error                    点数上限 / 坐标范围 / segments 长度
│
├─ planner.Compute(points, mode, manual, segments, m, am) → Result    internal/planner/planner.go
│   ├─ (s *Service) DistanceMatrix(points, mode)
│   │            → (dists [][]float64, degraded bool)                internal/matrix/matrix.go
│   │   ├─ (c *Client) drivingMatrix(points) → ([][]float64, error)  驾车：逐列并发（信号量限 4 路）
│   │   │    ├─ (c *Client) fetchColumn(origins, dest) → ([]float64, error)  一次 v3/distance
│   │   │    └─ wrapColumnErr(col, err) error                        CUQPS 限流错误加可操作提示
│   │   ├─ (c *Client) pairwiseMatrix(points, mode)                   步行/公交逐对 + 350ms 节流
│   │   └─ haversineMatrix(points)                                    降级：全直线矩阵
│   ├─ solver.SimulatedAnnealing(dists, 0) → []int                    internal/solver/tsp.go
│   │    ├─ NearestNeighbor(dists, start) → []int                     最近邻贪心粗解
│   │    └─ TwoOpt(dists, order) → []int                              断边反转，收到局部最优
│   └─ solver.TourLength(dists, order) → float64                      相邻距离求和
│
└─ matrix.Haversine(a, b model.Point) → float64                      混合出行单段降级复用
```

## 🧭 架构详解

### `POST /plan` 数据流

```mermaid
flowchart TD
    A["POST /plan（JSON）"] --> B{"JSON 能解析？"}
    B -->|"否"| E400["400 + error"]
    B -->|"是"| C["points = origin + destinations"]
    C --> V0{"mode 在白名单里？<br/>driving / walking / transit"}
    V0 -->|"否（如 cycling）"| E400
    V0 -->|"是"| D{"校验通过？<br/>点数 ≤ 50（驾车）/ ≤ 10（逐对）<br/>坐标范围合法<br/>segments 长度 = 点数-1 且方式合法"}
    D -->|"否"| E400
    D -->|"是"| F{"请求带 segments？"}

    F -->|"是：混合出行（每段方式不同）"| G["逐段 amap.Distance(a, b, segments[i])"]
    G --> H{"该段成功？"}
    H -->|"否"| I["仅该段降级为 haversine<br/>追加 warning + degraded"]
    H -->|"是"| J["累加距离"]
    I --> J

    F -->|"否：全程单一方式"| K["matrix.DistanceMatrix<br/>返回 (矩阵, 是否降级)"]
    K --> K2{"降级了？"}
    K2 -->|"是"| K3["整张矩阵降级 haversine<br/>追加一条覆盖全线的 warning"]
    K2 -->|"否"| L
    K3 --> L{"manual = true？"}
    L -->|"否：自动优化"| MM["solver.SimulatedAnnealing<br/>最近邻 → 2-opt → 退火"]
    L -->|"是：按用户列表顺序"| N["order = 0,1,2,..."]
    MM --> O["solver.TourLength 求总距离"]
    N --> O

    J --> R["planner.Result"]
    O --> R
    R --> P["200：order（名字）+ order_idx（下标）<br/>+ total_km + warnings + degraded<br/>is_degraded = 有没有 warning"]
```

两条业务约束：**混合出行不做 TSP**（每段方式不同，段间距离不可比，全局最优顺序无从谈起）；
**`order` 和 `order_idx` 都要返回**（名字不是标识符——搜两次"故宫"会有两个同名点，机器按下标画线）。

### 缓存与降级

缓存 key 带出行方式（`driving|lng,lat->lng,lat`），Redis 再加 `dist:` 前缀——
同一个点对不同方式距离不同，共用 key 会互相污染（缓存粒度必须匹配业务语义）。

| 故障点 | 典型原因 | 降级策略 | 日志前缀 |
|---|---|---|---|
| Redis 连接 | 进程没起 / 网络不通 | → 进程内 Memory 缓存（**不算降级**，不给用户报警告） | `redis unavailable` |
| 高德 API（矩阵） | 配额超限 / 超时 / `status=0` | → 整张矩阵 haversine，**回传 `is_degraded=true` + 全线一条 warning** | `[matrix] amap ... falling back` |
| 高德 API（混合出行某段） | 单段失败 | → 仅该段 haversine，该段名进 `degraded` | `[plan] 路段 ... 失败` |
| 非法 `mode` | 调用方拼错 | → **400 直接拒绝**（参数错 ≠ 外部依赖故障） | — |
| `/route` 轨迹 | 公交无连续轨迹 | → 返回空数组，前端该段画直线（业务事实，非错误） | — |

**降级必须可见**：`matrix.DistanceMatrix` 的第二个返回值 `degraded` 专门把"这张矩阵是降级来的"
带出包外——只写日志不够，日志在服务器上，用户看不到。`is_degraded` 统一取 `len(warnings) > 0`
（单一真相源）。另一个区分：**没配 key 走 haversine 是"设计如此"，不算降级不报警告**；
配了 key 但调用失败才是降级。

**缓存命中效果**（历史实测，广州塔 → 白云山 → 长隆 3 点驾车）：
首次 3 次高德请求 ≈ 0.33s；二次同路线 **0 次** ≈ 0.06s（加速比 ≈ 5.5×）；
重启 Go 服务后仍 **0 次**（Redis 进程还在，缓存跨进程存活）。

### 设计模式落点

| 模式 | 在项目里的落点 |
|---|---|
| **依赖注入** | `main.go` 装配所有依赖并注入，其他包不自己 `new` 外部资源 |
| **适配器** | `Cache` 接口适配 Redis / Memory；高德 REST 根地址是 `Client` 实例字段（`NewClientWithBase`），测试据此指向本地假服务器 |
| **策略** | 出行方式 = 不同高德接口与路径 |
| **模板方法** | TSP 固定流程：最近邻粗解 → 2-opt → 模拟退火 |
| **代理** | `/search`、`/route` 代理高德，第三方 key 不进前端 |
| **Repository** | `TaskRepo` 接口把存储关在后面，单测塞内存 fake 不用真库 |

### 高德接口路径表

全部常量收口在 `internal/amap/amap.go`：

| 常量 | 路径 | 用途 | 备注 |
|---|---|---|---|
| `pathDistance` | `/v3/distance` | 驾车距离矩阵 | 批量：多个起点 → 单个终点 |
| `pathDrivingDir` | `/v3/direction/driving` | 驾车单段距离 / 轨迹 | 含 `steps[].polyline` |
| `pathWalkingV5` | `/v5/direction/walking` | 步行单段距离 | 数值准，但**不返回轨迹** |
| `pathWalkingV3` | `/v3/direction/walking` | 步行轨迹 | 画线用它，只有它有轨迹 |
| `pathTransitDir` | `/v3/direction/transit/integrated` | 公交单段距离 | 轨迹不连续，不出 polyline |
| `pathPlaceText` | `/v3/place/text` | 关键词搜地点 | `/search` 与 agent 的 `search_place` 用它 |

> **同一个出行方式两个路径**不是笔误：步行距离用 v5（数据准），步行轨迹只能用 v3
> （v5 不返回坐标）。这类"文档看不出来、实测才知道"的细节，正是 `scripts/smoke.sh`
> 存在的理由——`httptest` 抓不到真实 API 的行为。

## 🗂️ 项目结构

```
route66/
├── main.go                 # 装配层:读配置、组装、启动
├── internal/
│   ├── api/                # Gin 路由: /plan /search /route /auth /plans + 编排
│   ├── agent/              # 手写 ReAct 智能体:loop(循环) + model(OpenAI兼容) + tools(业务工具)
│   ├── auth/               # 用户系统:注册/登录/会话(cookie) + 中间件
│   ├── planner/            # 业务规则收口:Validate + Compute(同步接口/worker/agent 三方共用)
│   ├── matrix/             # 距离矩阵(高德→haversine 降级,并上报"降级了")
│   ├── solver/             # 手写 TSP:最近邻→2-opt→模拟退火
│   ├── amap/               # 高德 Web 服务客户端(路径常量化,根地址可替换)
│   ├── cache/              # 缓存抽象:内存 / Redis
│   ├── queue/              # Redis Stream 任务队列(消费组 + ACK)
│   ├── worker/             # 异步规划后台消费者:领任务→解算→写库→ACK
│   ├── repo/               # PostgreSQL 持久化(GORM):tasks/users
│   ├── settings/           # 配置中心:高德 key 存 DB,管理台可热更换
│   ├── config/             # 环境变量配置
│   └── model/              # Point / Place / Mode
├── cmd/
│   └── agent-demo/         # agent 演示入口(独立 main,跑完即退)
├── frontend/               # 前端源码(React 19 + Vite)
│   ├── src/
│   │   ├── App.jsx             # 组合根:状态提升到这里,再往下分发
│   │   ├── api.js              # 后端接口唯一出口(fetch 封装 + 错误归一化)
│   │   ├── theme.js            # 设计变量的 JS 侧(地图折线要用)
│   │   ├── components/         # 组件:一个文件一个组件
│   │   ├── hooks/              # 状态逻辑(usePoints / useAmap / usePlan / useAuth …)
│   │   └── styles/             # tokens.css(设计变量) + app.css(组件样式)
│   ├── vite.config.js          # base:'/' + outDir:'../web'(两者必须同指一处) + dev 代理到 7800
│   └── package.json
├── admin/                  # 管理台(原生 HTML 单页,不进构建)
├── web/                    # ← 前端构建产物(gitignore,由 npm run build 生成)
├── docs/superpowers/       # 设计文档产物(本地参考,gitignore 不进仓库)
└── scripts/smoke.sh        # 真 key 冒烟测试(单测抓不到真实 API 行为)
```

前端地图默认视野是**北京**（`frontend/src/theme.js` 的 `DEFAULT_CENTER = [116.397, 39.909]`，
注意高德是**经度在前**），后续可接 `navigator.geolocation` 用真实定位覆盖它——代码里留了 TODO 说明注意事项。

**Go 那边为什么一行都不用改**：`internal/api/router.go` 里是
`r.NoRoute(http.FileServer(http.Dir("web")))` —— 它托管的是整个 `web/` 目录，
所以只要构建产物落在 `web/`，前端就自动出现在站点根路径 `/`，无需新增路由。

## ❓ 排查：几个"看着像 bug"的配置问题

配 key 这件事有两处地方、三样东西，最容易在这里耗时间。按现象对号入座：

| 现象 | 原因 | 怎么修 |
|---|---|---|
| 搜索弹「未配置 AMAP_KEY,搜索不可用」 | 后端启动时没读到 `AMAP_KEY`（它只认环境变量，不读网页里填的 key） | `export AMAP_KEY=...` 后**重启后端** |
| 后端报 `USERKEY_PLAT_NOMATCH`(10009) | 拿浏览器那把 JS key 当后端 key 用了 | 控制台给这把 key 追加「Web服务」平台 |
| 地图全白，提示「还没配置高德 Key」 | 当前浏览器里没填 JS key（它有**换浏览器就没了**的特性，因为存在 localStorage） | 在当前浏览器右上角「设置」里填一次 |
| 地图正常，但搜索永远失败（`INVALID_USER_SCODE`） | 漏填安全密钥 | 控制台复制安全密钥，填进「设置」 |
| 总距离明显偏小、像直线 | 后端降级到 haversine 了 | 看响应里的 `is_degraded` 和 `degraded`，它会告诉你哪几段不是真实路网 |

> 一句话记住分工：**网页「设置」= 地图；环境变量 `AMAP_KEY` = 距离、搜索、轨迹。**
> 两把 key 可以合成一把（同一个 key 勾两个平台）。

## 🎓 教学价值

这个项目是**边做边学 Go** 的完整载体，每个模块对应一个知识点：

| 模块 | 你学到 |
|---|---|
| `Gin` vs `net/http` | 为什么用框架、中间件 |
| `internal` 强制封装 | 高内聚低耦合 |
| `solver` | **TSP 优化算法入门**（贪心/局部搜索/启发式）|
| `amap` | 外部 API 客户端、JSON、超时、**优雅降级** |
| `cache` | 缓存模式、TTL、并发安全、**面向接口** |
| `queue` + `worker` | Redis Stream 消费组、ACK、任务状态机 |
| `repo` + `auth` | PostgreSQL、GORM、Repository 模式、bcrypt、防枚举 |
| `agent` | **ReAct 循环、函数调用协议、面向接口的可测试性** |
| 前后端分离 | 同源托管、无 CORS、API 代理模式 |

> 全程中文注释，讲「为什么」而不只是「怎么做」。

## 📄 License

[MIT](LICENSE)

---

<div align="center">
  <sub>Built with ❤️ and a lot of ☕ · Route 66 · 让每一条路都有最优解</sub>
</div>
