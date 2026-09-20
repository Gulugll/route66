# Route66

**多点路线规划服务 · Go · React 19 · 手写 TSP · 内置 ReAct 智能体**

`Go` · `Gin` · `高德地图` · `手写TSP` · `Redis` · `React 19` · `ReAct Agent`

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev/)
[![Gin](https://img.shields.io/badge/Gin-1.12-008ECF?style=for-the-badge&logo=gin&logoColor=white)](https://gin-gonic.com/)
[![高德](https://img.shields.io/badge/高德地图-API-red?style=for-the-badge&logo=amazonaws&logoColor=white)](https://lbs.amap.com/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=for-the-badge&logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)](LICENSE)

---

## 简介

Route66 是一个多点路径规划服务：输入一串地点，自动计算总距离最短的访问顺序（TSP），
支持驾车 / 步行 / 公交三种出行方式，以及每段路独立选择方式的混合出行。
距离数据来自高德 Web 服务 API，高德不可用时自动降级为 haversine 直线估算并在响应中显式标注。

后端为 Go 单进程（Gin + Redis + PostgreSQL），前端为 React 19 + Vite，
由 Go 进程同源托管，无 CORS。另提供一个基于手写 ReAct 循环的智能体，
可由 LLM 自主组合「地点搜索」与「路线规划」工具完成任务。

## 功能特性

| 特性 | 说明 |
|---|---|
| TSP 路径优化 | 手写求解器：最近邻 → 2-opt → 模拟退火 |
| 多出行方式 | 驾车 / 步行 / 公交，各走高德对应接口 |
| 混合出行 | 每段路独立指定方式（`segments` 参数） |
| 拖拽排序 | 原生 HTML5 DnD，零第三方依赖 |
| React 前端 | React 19 + Vite，受控组件 + hooks |
| 地图交互 | 高德 JS API 2.0：点选加地点、地名搜索、彩色路线与图例 |
| 双重缓存 | Redis 优先，不可用降级进程内 map，TTL 24h |
| 优雅降级 | 高德失败回退 haversine，降级信息随响应回传（`is_degraded`） |
| 认证与管理台 | PostgreSQL 可选：用户系统、API 级登录墙、高德 key 管理台热更换 |
| 异步任务 | Redis Stream 消费组 + 后台 worker，长任务提交后轮询 |
| 智能 Agent | 手写 ReAct 循环，模型自主调用业务工具，全过程可观察 |

## 架构

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

分层设计：`api`(编排) / `matrix`(距离来源) / `solver`(TSP) / `amap`(高德客户端) /
`cache`(缓存抽象)——低耦合，面向接口。距离来源、求解算法、缓存实现的替换均不影响调用方。

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
| 依赖方向单向，无环 | 保证包可独立测试、可独立演进为服务 |
| `solver` 只吃 `[][]float64`，不认识 `amap` | 算法与距离来源解耦 |
| `api` 不直接计算距离/排序 | 编排层只做参数校验与串联 |
| `amap` 依赖 `Cache` 接口，不认识 Redis | 依赖倒置：换缓存实现零改动 |
| `matrix` 位于 `amap` 与 `solver` 之间 | 降级策略集中在一处 |

## 快速开始

### 前置条件

- Go 1.26+
- （可选）Redis 7 —— 不装也能跑，自动降级内存缓存
- （可选）Node 20+ —— 仅构建/修改前端时需要（源码在 `frontend/`）
- （可选）PostgreSQL —— 启用认证、异步任务与管理台

### 1. 申请高德 key

高德开放平台 [console.amap.com](https://console.amap.com) → 实名认证 → 创建应用。

高德按「服务平台」分权：一个应用下申请一把 key，在「服务平台」里同时勾选两项即可。

| 平台 | 用途 | 配置位置 |
|---|---|---|
| **Web服务** | 后端距离 / 搜索 / 路网轨迹（REST） | 服务端环境变量 `AMAP_KEY` |
| **Web端(JS API)** | 浏览器渲染地图 | 管理台配置，经 `/config/public` 下发 |

**安全密钥**：2021-12-02 之后申请的 key，JS API 2.0 强制要求。典型现象是地图能渲染，
但搜索、距离类接口报 `INVALID_USER_SCODE`。

**报错码对照**：

| 高德返回 | 含义 | 处理 |
|---|---|---|
| `INVALID_USER_KEY` (10001) | key 不存在 | 核对 key，或确认未被删除 |
| `USERKEY_PLAT_NOMATCH` (10009) | key 未绑定目标平台 | 控制台为该 key 追加对应平台 |
| `INVALID_USER_SCODE` | 缺安全密钥 | 在 JS key 配置处一并填入 |

### 2. 启动

```bash
# 起 Redis(可选)
redis-server --port 6379 &

# 起后端
export AMAP_KEY=你的Web服务key
go run .

# 打开页面
open http://localhost:7800
```

### 2.5 登录系统 + 管理台（PostgreSQL，可选）

配置数据库后启用认证与管理能力；不配置则全部不启用，同步规划不受影响。

```bash
docker compose up -d          # postgres:16-alpine, 5432

# .env 追加:
# PG_DSN=postgres://routeplanner:routeplanner@localhost:5432/routeplanner?sslmode=disable
# ADMIN_PORT=7801
# ADMIN_USER=admin
# ADMIN_PASSWORD=改成你自己的

go run .
```

| 端口 | 内容 |
|---|---|
| `:7800` | 用户端（规划界面 + `/auth/*` + `/config/public` 下发 JS key） |
| `:7801` | 管理台（key 掩码查看/热更换、用户列表；非 admin 一律 403） |

- **key 优先级**：管理台(DB) > `.env` 兜底；管理台修改立即生效，无需重启
- **安全模型**：REST key 仅存服务端；JS key 经 `/config/public` 下发；
  密码 bcrypt 存储；登录失败统一文案（防用户名枚举）；会话为 HttpOnly cookie，
  cookie 不按端口隔离，故 7801 每条路由强制 `RequireAdmin`

### 3. 使用

1. 搜索地名或直接在地图上点选添加地点
2. 拖拽调整顺序（或交给算法自动优化）
3. 需要时为每段路指定出行方式
4. 开始规划，查看路线与总距离

### 4. 前端构建

`frontend/` 是 React 源码，构建产物输出到 `web/`（gitignore，不进仓库），由 Go 托管在站点根路径：

```bash
cd frontend
npm install
npm run build      # 产物输出到 ../web/（index.html + assets/）
```

开发时使用 Vite 开发服务器（热更新）：

```bash
cd frontend
npm run dev        # http://localhost:5173, /plan /search /route 已代理到 7800
```

> `vite.config.js` 里的 `base` 与 `outDir` 必须指向同一位置：
> `base` 决定 HTML 中资源路径写法，`outDir` 决定产物落盘位置，只改其一会导致资源 404。
>
> Go 侧托管逻辑在 `router.go`：`r.NoRoute(http.FileServer(http.Dir("web")))`，
> 产物落在 `web/` 即出现在根路径，无需新增路由。

## API

所有业务接口位于登录墙之后（装配了 PostgreSQL 时需先 `POST /auth/login`）。

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
    "mode": "driving",
    "segments": ["walking","transit"]
  }'
```

```json
{"order": ["天安门","故宫","天坛"],
 "order_idx": [0,1,2],
 "total_km": 6.8,
 "warnings": [],
 "degraded": [],
 "is_degraded": false}
```

> `order` 与 `order_idx` 同时返回：名字不是标识符（同名地点会重复），名字供人读，下标供机器定位。

| 参数 | 说明 |
|---|---|
| `origin` / `destinations` | 起点 + 目的地点列表 |
| `mode` | 全局出行方式（默认 `driving`）；白名单校验，非法值 400 |
| `segments` | 混合出行，`segments[i]` = 第 i 段方式（长度 = 点数-1） |
| `manual` | `true` 按列表顺序，跳过 TSP |

**点数上限按出行路径分档**：

| 路径 | 上限 | 原因 |
|---|---|---|
| 单一驾车 | 50 | `v3/distance` 批量接口，n 个点仅需 n 次请求 |
| 步行 / 公交 / 混合出行 | 10 | 逐对请求 + 350ms 节流，10 点约 32s |

降级显式回传：`is_degraded=true` + `warnings` 标明哪些数值是直线估算。

### `GET /search?q=关键词&city=可选` —— 地名搜索

```bash
curl "http://localhost:7800/search?q=故宫&city=北京"
```

```json
{"places":[{"name":"故宫博物院","address":"景山前街4号","lat":39.9163,"lng":116.3972}]}
```

### `GET /route?origin=lng,lat&dest=lng,lat&mode=driving` —— 路网轨迹

`origin` / `dest` 为 `经度,纬度` 顺序（与高德一致）。

```bash
curl "http://localhost:7800/route?origin=116.3975,39.9087&dest=116.3972,39.9163&mode=driving"
```

返回 `[lng,lat]` 坐标数组，前端拼为折线。`mode` 非法返回 400；`mode=transit` 返回空数组
（公交无连续轨迹，前端退化为直线，属业务事实而非错误）。

## 智能 Agent（手写 ReAct）

`internal/agent/` 实现一个手写的最小智能体：不依赖任何 Agent 框架，
核心循环为「调模型 → 执行模型请求的工具 → 结果回填历史 → 循环」，
直到模型返回纯文本即最终答案。

| 文件 | 角色 | 说明 |
|---|---|---|
| `internal/agent/types.go` | 类型 | `Message` / `ToolCall` / `ToolSpec`，对应 OpenAI Chat 协议消息形状 |
| `internal/agent/model.go` | 模型 | `Model` 接口 + `OpenAICompatible` 实现（DeepSeek / Kimi / Qwen 兼容模式 / Ollama，换厂商只改 BaseURL 与 Model） |
| `internal/agent/tool.go` | 工具 | `Tool` 接口：`Spec()` 提供说明书，`Run()` 执行 |
| `internal/agent/loop.go` | 循环 | `Agent.Run()`：ReAct 循环 + `MaxIterations` 防失控 + `OnStep` 逐轮回调 |
| `internal/agent/tools_plan.go` | 业务工具 | `search_place`（直通 `amap.Client.SearchPlaces`）、`plan_route`（直通 `planner.Compute`） |
| `cmd/agent-demo/main.go` | 装配层 | 独立入口，与 7800 服务互不依赖 |

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

### 设计要点

- **循环不做决策**：决策完全来自模型的每轮输出，`loop.go` 只负责执行、历史管理与防失控
- **工具层无业务逻辑**：校验、解算、降级全部复用 `planner.Compute`，与同步接口、异步 worker 走同一条解算链
- **失败喂回而非中断**：未知工具、非法参数、上游超时转为错误文本回填历史，由模型自行纠正；超过 `MaxIterations` 轮强制终止
- **多工具并发执行**：模型一次请求多个调用时 goroutine 并发执行，结果按下标对位回填
- **可测试性**：循环仅依赖 `Model` / `Tool` 两个小接口，注入假实现即可覆盖循环逻辑（`-race` 下 4 个场景通过）

### 运行

```bash
set -a; source .env; set +a
export LLM_BASE_URL=https://api.deepseek.com   # 任选 OpenAI 兼容服务
export LLM_API_KEY=sk-xxx
export LLM_MODEL=deepseek-chat
go run ./cmd/agent-demo "帮我规划一条从天安门出发,途经故宫再到天坛的驾车路线"
```

未配置 `AMAP_KEY` 时规划工具走 haversine，agent 会在回答中说明是直线估算。

## 运行时：启动与请求流转

### 启动时序（`main.go` 为装配层）

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
    C-->>M: Config: Port / AMAP_KEY / RedisAddr / PG_DSN 等
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
        Note over M: 认证/异步/管理台不启用<br/>同步 /plan 照常
    end
    M->>AM: NewClient(key, distCache).WithKeyFn(动态取 key)
    Note over AM: key 每次请求现查（DB > env）<br/>管理台修改不重启即生效
    M->>MX: NewWithAmap(amapClient)
    M->>API: NewRouter(...) + NoRoute 托管 web/
    M->>M: router.Run(":7800")
    Note over M: 阻塞在 net/http Serve() 的 Accept 循环<br/>每个连接派发独立 goroutine，进程驻留
```

- Redis 连接失败不报错不退出，降级进程内缓存，任一外部依赖不可用服务照常运行
- 装配出的 `matrixService` / `amapClient` 为进程级单例，所有请求共享（缓存跨请求命中的前提）
- `router.Run()` 阻塞在标准库 `net/http.Serve()` 的 Accept 循环，每个连接派发独立 goroutine

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
    P->>P: ShouldBindJSON + ParseMode + Validate
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
    P-->>FE: 200 JSON
```

### `/plan` 内部调用链

```
(s *Server) plan(c *gin.Context)                                     api/router.go
├─ planner.ParseMode(s string) → (model.Mode, error)                 mode 白名单，空 = 默认 driving
├─ planner.Validate(points, mode, segments) error                    点数上限 / 坐标范围 / segments 长度
│
├─ planner.Compute(points, mode, manual, segments, m, am) → Result    internal/planner/planner.go
│   ├─ (s *Service) DistanceMatrix(points, mode)
│   │            → (dists [][]float64, degraded bool)                internal/matrix/matrix.go
│   │   ├─ (c *Client) drivingMatrix(points) → ([][]float64, error)  驾车：逐列并发（信号量限 4 路）
│   │   │    ├─ (c *Client) fetchColumn(origins, dest) → ([]float64, error)
│   │   │    └─ wrapColumnErr(col, err) error                        CUQPS 限流错误附处理提示
│   │   ├─ (c *Client) pairwiseMatrix(points, mode)                   步行/公交逐对 + 350ms 节流
│   │   └─ haversineMatrix(points)                                    降级：全直线矩阵
│   ├─ solver.SimulatedAnnealing(dists, 0) → []int                    internal/solver/tsp.go
│   │    ├─ NearestNeighbor(dists, start) → []int
│   │    └─ TwoOpt(dists, order) → []int
│   └─ solver.TourLength(dists, order) → float64
│
└─ matrix.Haversine(a, b model.Point) → float64                      混合出行单段降级复用
```

## 架构详解

### TSP 求解器

求解管线为三级：最近邻贪心粗解 → 2-opt 局部搜索 → 模拟退火跳出局部最优。
实测（广州三点基准，驾车）：最近邻 322.44 km → +2-opt 289.14 km → +模拟退火 285.62 km。
2-opt 的好坏判据使用精确的 `TourLength` 差值而非「边界两条边」简化公式——
驾车真实路网距离矩阵不对称（单行道/禁转），简化公式会漏掉内部边翻转的收益。

### `POST /plan` 数据流

```mermaid
flowchart TD
    A["POST /plan（JSON）"] --> B{"JSON 能解析？"}
    B -->|"否"| E400["400 + error"]
    B -->|"是"| C["points = origin + destinations"]
    C --> V0{"mode 在白名单里？"}
    V0 -->|"否"| E400
    V0 -->|"是"| D{"校验通过？<br/>点数 ≤ 50（驾车）/ ≤ 10（逐对）<br/>坐标范围合法<br/>segments 长度 = 点数-1 且方式合法"}
    D -->|"否"| E400
    D -->|"是"| F{"请求带 segments？"}

    F -->|"是：混合出行"| G["逐段 amap.Distance(a, b, segments[i])"]
    G --> H{"该段成功？"}
    H -->|"否"| I["仅该段降级为 haversine<br/>追加 warning + degraded"]
    H -->|"是"| J["累加距离"]
    I --> J

    F -->|"否：单一方式"| K["matrix.DistanceMatrix<br/>返回 (矩阵, 是否降级)"]
    K --> K2{"降级了？"}
    K2 -->|"是"| K3["整张矩阵降级 haversine<br/>追加全线一条 warning"]
    K2 -->|"否"| L
    K3 --> L{"manual = true？"}
    L -->|"否"| MM["solver.SimulatedAnnealing"]
    L -->|"是"| N["order = 0,1,2,..."]
    MM --> O["solver.TourLength 求总距离"]
    N --> O

    J --> R["planner.Result"]
    O --> R
    R --> P["200：order + order_idx + total_km<br/>+ warnings + degraded + is_degraded"]
```

两条业务约束：

- **混合出行不做 TSP**：每段方式不同则段间距离不可比，统一口径的距离矩阵不存在，全局最优顺序无从谈起，因此固定按列表顺序逐段计算
- **`order` 与 `order_idx` 同时返回**：名字供人读，下标供机器定位（前端按下标画线）

### 缓存与降级

缓存 key 携带出行方式（`driving|lng,lat->lng,lat`），Redis 侧再加 `dist:` 前缀——
同一地点对不同方式距离不同，共用 key 会互相污染。

| 故障点 | 典型原因 | 降级策略 | 日志前缀 |
|---|---|---|---|
| Redis 连接 | 进程未起 / 网络不通 | → 进程内 Memory 缓存（不产生用户侧警告） | `redis unavailable` |
| 高德 API（矩阵） | 配额超限 / 超时 / `status=0` | → 整张矩阵 haversine，回传 `is_degraded=true` + 全线 warning | `[matrix] amap ... falling back` |
| 高德 API（混合出行某段） | 单段失败 | → 仅该段 haversine，该段名进 `degraded` | `[plan] 路段 ... 失败` |
| 非法 `mode` | 调用方拼错 | → 400 拒绝（参数错误不降级） | — |
| `/route` 轨迹 | 公交无连续轨迹 | → 返回空数组，前端该段画直线 | — |

降级可见性设计：`matrix.DistanceMatrix` 的第二个返回值 `degraded` 将「矩阵来自降级」这一事实
带出包外——日志仅服务端可见，降级信息必须随 HTTP 响应回传。`is_degraded` 统一取
`len(warnings) > 0`（单一真相源）。注意区分：未配置 `AMAP_KEY` 时走 haversine 是预期行为，
不产生警告；配置了 key 但调用失败才是降级。

缓存命中效果（广州塔 → 白云山 → 长隆，3 点驾车）：
首次 3 次高德请求约 0.33s；二次同路线 0 次约 0.06s；重启 Go 进程后仍 0 次（Redis 跨进程存活）。

### 设计模式落点

| 模式 | 落点 |
|---|---|
| 依赖注入 | `main.go` 装配全部依赖，其他包不自建外部资源 |
| 适配器 | `Cache` 接口适配 Redis / Memory；高德根地址为 `Client` 实例字段（`NewClientWithBase`），测试指向本地假服务器 |
| 策略 | 出行方式对应不同高德接口与路径 |
| 模板方法 | TSP 固定管线：最近邻 → 2-opt → 模拟退火 |
| 代理 | `/search`、`/route` 代理高德，凭据不进前端 |
| Repository | `TaskRepo` 接口隔离存储，单测注入内存实现 |

### 高德接口路径表

全部常量收口在 `internal/amap/amap.go`：

| 常量 | 路径 | 用途 | 备注 |
|---|---|---|---|
| `pathDistance` | `/v3/distance` | 驾车距离矩阵 | 批量：多起点 → 单终点 |
| `pathDrivingDir` | `/v3/direction/driving` | 驾车单段距离 / 轨迹 | 含 `steps[].polyline` |
| `pathWalkingV5` | `/v5/direction/walking` | 步行单段距离 | 数据准，不返回轨迹 |
| `pathWalkingV3` | `/v3/direction/walking` | 步行轨迹 | 画线专用（v5 无轨迹） |
| `pathTransitDir` | `/v3/direction/transit/integrated` | 公交单段距离 | 轨迹不连续，无 polyline |
| `pathPlaceText` | `/v3/place/text` | 关键词搜索 | `/search` 与 agent 的 `search_place` |

> 步行距离用 v5（数据准），轨迹只能用 v3（v5 不返回坐标）——此类差异依赖
> `scripts/smoke.sh` 真实请求验证，`httptest` 无法覆盖真实 API 行为。

## 项目结构

```
route66/
├── main.go                 # 装配层:读配置、组装、启动
├── internal/
│   ├── api/                # Gin 路由: /plan /search /route /auth /plans + 编排
│   ├── agent/              # 手写 ReAct 智能体:loop + model(OpenAI兼容) + tools(业务工具)
│   ├── auth/               # 用户系统:注册/登录/会话(cookie) + 中间件
│   ├── planner/            # 业务规则收口:Validate + Compute(同步接口/worker/agent 共用)
│   ├── matrix/             # 距离矩阵(高德→haversine 降级并上报)
│   ├── solver/             # 手写 TSP:最近邻→2-opt→模拟退火
│   ├── amap/               # 高德 Web 服务客户端
│   ├── cache/              # 缓存抽象:内存 / Redis
│   ├── queue/              # Redis Stream 任务队列(消费组 + ACK)
│   ├── worker/             # 异步规划后台消费者
│   ├── repo/               # PostgreSQL 持久化(GORM):tasks/users
│   ├── settings/           # 配置中心:高德 key 存 DB,管理台热更换
│   ├── config/             # 环境变量配置
│   └── model/              # Point / Place / Mode
├── cmd/
│   └── agent-demo/         # agent 演示入口(独立 main)
├── frontend/               # 前端源码(React 19 + Vite)
│   ├── src/
│   │   ├── App.jsx             # 组合根
│   │   ├── api.js              # 后端接口唯一出口(fetch 封装 + 错误归一化)
│   │   ├── theme.js            # 设计变量的 JS 侧(地图折线使用)
│   │   ├── components/         # 组件
│   │   ├── hooks/              # usePoints / useAmap / usePlan / useAuth ...
│   │   └── styles/             # tokens.css + app.css
│   ├── vite.config.js          # base:'/' + outDir:'../web' + dev 代理到 7800
│   └── package.json
├── admin/                  # 管理台(原生 HTML 单页)
├── web/                    # 前端构建产物(gitignore)
├── docs/superpowers/       # 设计文档产物(gitignore)
└── scripts/smoke.sh        # 真实 key 冒烟测试
```

前端地图默认视野为北京（`frontend/src/theme.js` 的 `DEFAULT_CENTER = [116.397, 39.909]`，
高德坐标为经度在前），可接 `navigator.geolocation` 以真实定位覆盖。

## 排查

配 key 涉及两处位置、多类凭据，常见问题按现象对号入座：

| 现象 | 原因 | 处理 |
|---|---|---|
| 搜索返回「未配置 AMAP_KEY,搜索不可用」 | 后端启动时未读到 `AMAP_KEY` | `export AMAP_KEY=...` 后重启后端 |
| 后端报 `USERKEY_PLAT_NOMATCH`(10009) | 使用了未绑定 Web服务平台的 key | 控制台为该 key 追加「Web服务」平台 |
| 地图空白，提示「还没配置高德 Key」 | 当前浏览器未获取 JS key | 管理台配置后刷新页面 |
| 地图正常但搜索失败（`INVALID_USER_SCODE`） | 缺安全密钥 | 在 JS key 配置处补填 |
| 总距离明显偏小、呈直线 | 后端降级到 haversine | 查看响应的 `is_degraded` 与 `degraded` |

分工：**服务端 `AMAP_KEY` = 距离、搜索、轨迹；JS key = 地图渲染**。
两把 key 可合并为一把（同一 key 勾选两个平台）。

## License

[MIT](LICENSE)
