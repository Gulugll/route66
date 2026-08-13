# 路线规划系统设计文档（Go 学习项目）

日期：2026-08-13
状态：已确认，待实现

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
| 日志 | log/slog（标准库） | 零依赖，学结构化日志 |
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

- 统一 error 类型 + gin 中间件收口 + slog 记日志
- 高德 API 失败 → 指数退避重试 → 降级为 haversine 直线距离（保证演示不挂）
- 测试：
  - 解算器：断言结果是合法排列 + 小规模已知最优
  - 缓存命中/回源逻辑
  - handler：httptest + 假矩阵服务
  - 最后 docker-compose 起全套做集成测试

## 7. 明确不做（YAGNI）

- Kafka、服务发现（etcd/consul）、可观测性全家桶（otel/zap）、认证授权
- 完整前端项目（React/Vue）—— 后面想学再加
- OR-Tools / LKH 等重型解算器 —— 手写够用时不用
