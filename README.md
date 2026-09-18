<div align="center">

# 🛣️ Route66

**多点路线智能规划器 · 手写 TSP · 多出行方式 · 极客教学项目**

`Go` · `Gin` · `高德地图` · `手写TSP` · `Redis` · `React 19`

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
> 用 SortableJS 拖拽）已经删除，但它的教训留在了 `AGENTS.md` 和 `docs/architecture-diagrams.md`：
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

## 🗂️ 项目结构

```
route66/
├── main.go                 # 装配层:读配置、组装、启动
├── internal/
│   ├── api/router.go       # Gin 路由: /plan /search /route + 参数校验 + 降级上报
│   ├── matrix/             # 距离矩阵(高德→haversine 降级,并上报"降级了")
│   ├── solver/             # 手写 TSP:最近邻→2-opt→模拟退火
│   ├── amap/               # 高德 Web 服务客户端(路径常量化,根地址可替换)
│   ├── cache/              # 缓存抽象:内存 / Redis
│   ├── config/             # 环境变量配置
│   └── model/              # Point / Place / Mode
├── frontend/               # 前端源码(React 19 + Vite)
│   ├── src/
│   │   ├── App.jsx             # 组合根:状态提升到这里,再往下分发
│   │   ├── api.js              # 后端接口唯一出口(fetch 封装 + 错误归一化)
│   │   ├── theme.js            # 设计变量的 JS 侧(地图折线要用)
│   │   ├── components/         # 组件:一个文件一个组件
│   │   ├── hooks/              # 状态逻辑(usePoints / useAmap / usePlan …)
│   │   └── styles/             # tokens.css(设计变量) + app.css(组件样式)
│   ├── vite.config.js          # base:'/' + outDir:'../web'(两者必须同指一处) + dev 代理到 7800
│   └── package.json
├── web/                    # ← 构建产物(gitignore,由 npm run build 生成) —— 托管在站点根路径
│   ├── index.html
│   └── assets/
├── docs/
│   ├── architecture-diagrams.md   # 架构图(Mermaid) + 设计原则
│   └── superpowers/specs/         # 最初的设计文档
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
| 前后端分离 | 同源托管、无 CORS、API 代理模式 |

> 全程中文注释，讲「为什么」而不只是「怎么做」。

## 📄 License

[MIT](LICENSE)

---

<div align="center">
  <sub>Built with ❤️ and a lot of ☕ · Route 66 · 让每一条路都有最优解</sub>
</div>
