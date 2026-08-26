<div align="center">

# 🛣️ Route66

**多点路线智能规划器 · 手写 TSP · 多出行方式 · 极客教学项目**

`Go` · `Gin` · `高德地图` · `手写TSP` · `Redis` · `原生前端`

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
| 🖱️ **拖拽排序** | SortableJS 原生实现 | 想手动控制顺序就手动，想优化就交给算法 |
| 🗺️ **地图点选** | 高德 JS API | 点地图即加地点，搜索地名自动带坐标 |
| 🎨 **彩色路线 + 图例** | 每段一种颜色 | 颜色即出行方式，一眼看懂 |
| ⚡ **双重缓存** | Redis + 内存 | 缓存命中零外部请求，重启跨进程存活 |
| 🛡️ **优雅降级** | 高德↔haversine | 外部 API 挂了服务照跑，矩阵不混搭 |

## 📸 效果预览

```
┌─────────────────────────┬──────────────────────────────────┐
│  🔍 搜索添加地点          │   🗺️  高德地图                    │
│  [广州塔      ] [搜索]     │        ·· 起点                    │
│  [城市(可选)          ]   │     🟦━━━━━┓                      │
│  ───────────────         │     🟩━━━━▓▓▓  (步行)             │
│  ✓ 起点  广州塔          │     🟧━━━━━━━┛  (公交)             │
│    ──🚗🚶🚌──            │                                │
│  ✓ 第1站  白云山          │   [图例] 🟦驾车 🟩步行 🟧公交       │
│    ──🚗🚶🚌──            │                                │
│  ✓ 第2站  长隆            │                                │
│  ☐ 手动设置每段出行方式✓    │                                │
│  [🚀 开始规划]            │                                │
└─────────────────────────┴──────────────────────────────────┘
```

## 🏗️ 架构

```
                    ┌──────────────────────────────┐
  浏览器(前端)        │        Go 后端 (Gin)          │
  ┌─────────┐       │  ┌────────────────────────┐  │      ┌────────────┐
  │ HTML/JS  │──────▶│  │  /plan  /search /route │  │─────▶│  高德 Web   │
  │ 高德JSAPI│       │  │   (API 代理层)          │  │      │  服务 API   │
  │ Sortable │       │  └──────────┬─────────────┘  │      └────────────┘
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

### 1. 申请高德 key（免费）

高德开放平台 [console.amap.com](https://console.amap.com) → 实名认证 → 创建应用 → 添加 **两个 key**：

| Key 类型 | 平台 | 用途 | 配置方式 |
|---|---|---|---|
| **Web 服务 key** | Web服务 | 后端算距离/搜索/路线 | 环境变量 `AMAP_KEY` |
| **JS API key** | Web端(JS API) | 前端地图渲染 | 网页「设置」里填 |

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

打开页面 → 右上角「设置」→ 填 JS API key → 保存重载。

### 3. 用起来

1. **搜索地名**（如"广州塔"）→ 点选添加
2. 或者直接在**地图上点**加地点
3. **拖拽**调整顺序（想手动控制就勾选继续）
4. 每段路上选 🚗🚶🚌 出行方式
5. **🚀 开始规划** → 看彩色路线 + 总距离

## 📡 API

### `POST /plan` —— 规划路线

```bash
curl -X POST http://localhost:7800/plan \
  -H 'Content-Type: application/json' \
  -d '{
    "origin": {"name":"广州塔","lat":23.1066,"lng":113.3245},
    "destinations": [
      {"name":"白云山","lat":23.1835,"lng":113.3042},
      {"name":"长隆","lat":23.0012,"lng":113.3274}
    ],
    "mode": "driving",              // driving | walking | transit
    "segments": ["walking","transit"]  // 混合出行:每段一种方式
  }'
```

```json
{"order":["广州塔","长隆","白云山"], "total_km":45.44}
```

| 参数 | 说明 |
|---|---|
| `origin` / `destinations` | 起点 + 目的地点列表 |
| `mode` | 全局出行方式（默认 `driving`）|
| `segments` | 混合出行，`segments[i]` = 第 i 段方式（长度 = 点数-1）|
| `manual` | `true` 按列表顺序，跳过 TSP |

### `GET /search?q=关键词&city=可选` —— 地名搜索

```bash
curl "http://localhost:7800/search?q=西湖&city=杭州"
```
```json
{"places":[{"name":"杭州西湖风景名胜区","address":"...","lat":30.24,"lng":120.14}]}
```

### `GET /route?origin=lng,lat&dest=lng,lat&mode=driving` —— 真实路网轨迹

返回一串 `[lng,lat]` 坐标，前端拼成折线。

## 🗂️ 项目结构

```
route66/
├── main.go                 # 装配层:读配置、组装、启动
├── internal/
│   ├── api/router.go       # Gin 路由: /plan /search /route
│   ├── matrix/             # 距离矩阵(高德→haversine 降级)
│   ├── solver/             # 手写 TSP:最近邻→2-opt→模拟退火
│   ├── amap/               # 高德 Web 服务客户端
│   ├── cache/              # 缓存抽象:内存 / Redis
│   ├── config/             # 环境变量配置
│   └── model/              # Point / Place / Mode
└── web/                    # 前端(无框架,原生 HTML/JS)
    ├── index.html
    ├── app.js
    └── vendor/sortable.min.js
```

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
