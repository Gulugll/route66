# 路线规划系统设计文档（Go 学习项目）

日期：2026-08-13
状态：已确认。**Phase 1 已实现**（2026-09）；Phase 2 / 3 待实现。
> ⚠️ 本文是**当初的设计意图**，不是当前实现的说明书。两者已有偏差，逐条列在文末
> 「附录：与实现的偏差（2026-09-14 核对）」。当前实现的权威描述见
> `docs/architecture-diagrams.md`。

## 1. 项目目标

构建一个"多点路线自动规划"系统：用户输入起点和若干想去的地点，系统自动算出最优游览顺序并给出多条可选方案。

**真实目的**：作为学习 Go 语言与中间件/组件的载体。业务是载体，学习是目标。

## 2. 学习目标（按阶段）

用户选择的中间件/组件栈（全部要学）：
- Web 框架 + HTTP（gin、REST API、中间件）
- 缓存与数据库（Redis、MySQL）
- 异步与消息队列（Redis Stream）
- gRPC / 微服务拆分

## 3. 关键决策记录

| 决策点 | 选择 | 理由 |
|---|---|---|
| 架构路线 | 方案 A 分阶段演进 | 每阶段完整可跑，学习曲线平缓，问题可定位 |
| 地图数据源 | 高德地图 API | 国内路网数据准，免费额度够学习 |
| TSP 解算器 | **手写**（最近邻 + 2-opt + 模拟退火） | OR-Tools Go 绑定需 cgo 安装痛苦；手写是极佳 Go 练习 |
| MQ | Redis Streams | 零新增基础设施（Redis 已有），消费组/ack/重试齐全 |
| 数据库 | MySQL（docker） | 接近生产，学连接池与迁移 |
| 界面 | API + 简单 HTML 页面 | 高德 JS 地图画路线 |
| 日志 | `log`（标准库）| 零依赖。*原定为 `log/slog`，实际没用上（见附录）* |
| 配置 | os.Getenv + 简单 config | 学习阶段不引 viper |
| 高德客户端 | 手写 net/http client | 学 HTTP 客户端写法 |
| 测试 | 标准库 testing + httptest | 不引框架 |

## 4. 架构与阶段

### Phase 1 — MVP（单进程 monolith）

```
curl POST /plan  {起点, [地点...]}
   → geocoding 拿坐标（高德）
   → 算距离矩阵（高德矩阵 API）→ Redis 缓存命中检查
   → TSP 解算器出最优顺序
   → 按顺序调路线 API 拿 polyline
   → 返回 {顺序, 每段耗时, 总耗时, 地图链接}
```

技术栈：gin + log/slog + os.Getenv + go-redis + 手写高德 client + 手写 TSP。

交付物：
- 可运行的 HTTP 服务
- 简单 HTML 页面（高德 JS 地图画路线）
- 单元测试（解算器、缓存、handler）

### Phase 2 — 异步 + 数据库

```
POST /plans                → 存 DB(status=queued) → XADD 任务 → 返回 plan_id
worker (XREADGROUP)        → 消费任务 → 解算 → 更新 DB(status=done, result JSON)
GET /plans/:id             → 前端轮询，拿到结果
```

- Redis Streams 做消息队列（消费组、ack、重试）
- MySQL + `database/sql` + `go-sql-driver/mysql`，手写连接池配置与迁移 SQL
- Repository 模式：DB 访问包成 interface 注入 handler（学接口设计）

### Phase 3 — 拆分 gRPC 微服务

```
[api 服务]  gin HTTP   ← 入口
   ├─ gRPC ──► [matrix 服务]   高德矩阵 + Redis 缓存
   └─ gRPC ──► [solver 服务]   TSP 解算
```

- Proto 定义接口契约（学 protobuf + codegen）
- 服务地址用 env 配置（暂不搞服务发现）
- Phase 1/2 的进程内函数调用位置即 gRPC 边界，拆时只换调用方式，业务逻辑不重写

## 5. 数据模型（Phase 2 起）

```
plans: id, origin(JSON), destinations(JSON),
       status(queued|done|failed), result(JSON),
       created_at, updated_at
```

## 6. 错误处理与测试

- 统一 error 类型 + gin 中间件收口 + 日志记错（**实际用 `log`，不是 `slog`**，见附录）
- 高德 API 失败 → 降级为 haversine 直线距离（保证演示不挂）
  - ⚠️ 原设计写了"指数退避重试"，**目前未实现**：现在是"失败即降级"。见附录
- **降级必须回传响应，不能只写日志**：`warnings` / `is_degraded` 让用户知道哪些数字是估算的
  （2026-09-14 补：这条以前只做到了一半，矩阵整体降级时没有上报）
- 非法参数一律 **400**，不做"猜一个默认值"的处理
- 测试：
  - 解算器：断言结果是合法排列 + 小规模已知最优
  - 缓存命中/回源逻辑
  - handler：httptest + 假矩阵服务 ✅（2026-09-14 补：`internal/api/router_test.go`）
  - 最后 docker-compose 起全套做集成测试

## 7. 明确不做（YAGNI）

- Kafka、服务发现（etcd/consul）、可观测性全家桶（otel/zap）、认证授权
- 完整前端项目（React/Vue）—— 后面想学再加
- OR-Tools / LKH 等重型解算器 —— 手写够用时不用

---

## 附录：与实现的偏差（2026-09-14 核对）

写代码的过程本身就是设计的一部分。以下偏差**不是 bug，而是设计被现实修正的痕迹**，
逐条记下来，免得下次读这份文档时把"当初的想法"当成"现在的行为"。

| # | 设计原文 | 实际实现 | 建议 |
|---|---|---|---|
| 1 | 日志用 `log/slog` | 全程 `log.Printf`，够用 | 保持现状；要学结构化日志时再单独一阶段迁 |
| 2 | 高德失败 → **指数退避重试** → 降级 | **没有重试**，失败即降级 | 明确取舍：本项目优先"快速降级不挂"；重试属于 Phase 2 之后的可靠性话题 |
| 3 | 数据模型 `plans: origin(JSON), destinations(JSON), status(queued\|done\|failed)…` | 表名改为 **`tasks`**（`req_json` / `result_json` / `error` + `idx_status_created`） | 见 `AGENTS.md` Phase 2 的表设计 |
| 4 | `POST /plans` → 返回 `plan_id`；`GET /plans/:id` 轮询 | 仍是同步的 `POST /plan` | Phase 2 才改；目标保持"提交即返回 + 轮询" |
| 5 | 前端只提到"高德 JS 地图画路线" | 加了**混合出行**（每段独立方式）、拖拽排序、段连接线 UI、降级警告框 | 已超出原设计范围，属合理演进 |
| 6 | — | **新增**：`mode` 白名单校验、点数上限分档（驾车 50 / 逐对 10）、`order_idx` | 2026-09-14 修复 |

### 还没做、但已经知道是债的

- **指数退避重试**（第 2 条）：高德偶发超时其实重试一次就能成功，现在直接降级了。
  做的话要注意"只重试幂等请求 + 重试上限 + 加抖动"。
- **缓存击穿**：并发两个请求问同一对未缓存的点，会同时打高德（`cache` 层没有 singleflight）。
  教学规模无所谓，Phase 2 做异步 worker 时可以顺手把"同一任务去重"一起解决。
- **`web/` 用相对路径托管**（`http.FileServer(http.Dir("web"))`）：从别的目录启动二进制页面会 404
  （API 正常）。已在启动日志里打出解析后的绝对路径便于排查；彻底解决可以上 `go:embed`，
  但那样改前端就得重新编译，权衡后暂不做。
- **前端 XSS 面**：已给所有拼接外部文本的地方加 `esc()`；更彻底的做法是全面改用
  `textContent` / `createElement`，不再拼 HTML 字符串。
