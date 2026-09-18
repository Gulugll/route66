# 方案：登录系统 + 双端（用户/管理）+ Key 中心化管理 + PostgreSQL

> 2026-09-17 起草 · 决策点 D1~D7 全部按推荐项落地
> 状态：**已实现（2026-09-17，P1~P6）**——go test ./... -race 全绿；
> smoke 16/16（原 12 项 + 认证可选段 4 项）；curl 走查全部认证流 +
> key 热生效（坏 key → /search 立即 502 → 清除 → 回落 env → 200，全程未重启）；
> 浏览器实测管理台登录/掩码回显/用户列表正常。

## 1. 需求还原

1. 登录系统（账号密码）；
2. 两个端：**用户端** = 现在的路线规划界面；**管理端** = 独立界面，管理
   高德 Web服务 key、JS key（+安全密钥）——现在散在 `.env` 和浏览器 localStorage 里；
3. key 的优先级：**管理端填写的 > .env 兜底**；
4. 数据库换 **PostgreSQL**（自建），连接配置放 `.env`，不用 MySQL。

## 2. 目标 / 非目标

**目标**：注册/登录/登出、角色区分（user/admin）、key 存 DB 且热生效（env 只兜底）、
管理端独立端口、repo 换 PG。
**非目标（本轮不做）**：邮箱验证/找回密码、OAuth 第三方登录、HTTPS（本地教学）、
限流防刷、用户端"历史记录"业务（登录的地基打好，那是下一块业务）。

## 3. 总体设计

```
                    ┌─ :7800 用户端 ─────────────────────────┐
浏览器(游客/用户) ──▶│ /plan /search /route /auth/*  + web/   │
                    └────────────────────────────────────────┘
                    ┌─ :7801 管理端 ─────────────────────────┐
管理员浏览器 ──────▶│ /admin/api/keys /admin/login + 管理页  │
                    └────────────────────────────────────────┘
                         │                    │
                         ▼                    ▼
                    PostgreSQL(用户/会话/配置/任务)
                         │
                    amap.Client 的 key 从 KeyProvider 动态取
                    （DB 有用 DB，DB 空回落 .env）
```

### 3.1 两个端口，一个进程 ⭐

两个 `gin.Engine` 各自挂 `http.Server`，在 `main` 里各跑一个 goroutine。
**为什么真的分端口而不是同端口分路径**：管理端口可以在网络层隔离
（将来上线只把 7801 绑 `127.0.0.1` 或加防火墙，公网根本摸不到），
比纯靠中间件的角色校验多一层防线——安全上"网络边界 > 应用边界"。

⚠️ **Cookie 的坑（教学点）**：cookie 按 host 隔离、**不按端口隔离**——
在 7800 登录拿到的会话 cookie，7801 会自动带上。所以两端**天然共享登录态**，
管理端只做"角色校验"（非 admin → 403），不用再登录一次。这是便利也是风险：
用户端机器上的任何脚本都能带着会话打管理端口，所以管理端必须每条路由都过 `RequireAdmin`。

### 3.2 表结构（GORM AutoMigrate，PG）

```sql
users       (id BIGSERIAL PK, username VARCHAR(64) UNIQUE NOT NULL,
             password_hash TEXT NOT NULL,          -- bcrypt,永存明文
             role VARCHAR(16) DEFAULT 'user',      -- user | admin
             created_at TIMESTAMPTZ DEFAULT now())

sessions    (token TEXT PK,                        -- 32B 随机数 hex,HttpOnly cookie
             user_id BIGINT REFERENCES users(id),
             expires_at TIMESTAMPTZ, created_at)   -- 过期清理顺手做

app_settings(key VARCHAR(64) PRIMARY KEY,          -- amap_rest_key / amap_js_key / amap_js_sec
             value TEXT NOT NULL, updated_at)      -- value 允许空串 = "清除,回落 env"

tasks       (现有表,AutoMigrate 平移到 PG,教学数据不迁移)
```

### 3.3 KeyProvider：DB > env，热生效 ⭐

```go
// internal/settings
type Provider struct { db *gorm.DB; fallback config.Config }
// Get(key) 先查 app_settings,空/无记录回落 env;写路径用 atomic.Value 缓存
// 避免每次请求都打 DB —— 管理端改 key 时更新缓存,立即生效,不用重启
```

- `amap.Client` 改造：`key string` 字段改为从 Provider 取（构造时注入），
  管理端换 key **不重启即生效**（下一请求就用新 key；旧缓存键带 key 前缀的话要注意缓存 key 生成）
- JS key / 安全密钥：新增 `GET /config/public` 返回给浏览器渲染地图
  （JS key 本来就暴露在浏览器里,不算泄密；REST key **永远不会**下发给前端）
- 前端 `useSettings` 改三层优先级：**localStorage 个人覆盖 > DB(管理端配的) > 无**
  设置弹层保留,提示"留空则使用管理员配置"

### 3.4 认证方案（internal/auth）

- 密码：`golang.org/x/crypto/bcrypt`（成本 10）——教学点：为什么不能 MD5、
  为什么 bcrypt 自带盐、`CompareHashAndPassword` 是常数时间比较
- 会话：服务端 session 表 + **HttpOnly cookie**（`Path=/; HttpOnly; SameSite=Lax`，
  7 天过期）——不选 JWT 的理由：能主动踢人/改角色即时生效，教学上也把
  "无状态 token vs 有状态会话"讲清楚
- 中间件：`OptionalAuth`（解析出用户,不拦）、`RequireAuth`、`RequireAdmin`
- **admin 引导**：启动时读 `ADMIN_USER`/`ADMIN_PASSWORD`（env）,不存在则种子创建——
  避免"第一个注册的人自称 admin"的鸡生蛋问题

### 3.5 管理端界面

推荐**单页原生 HTML+JS（不进 Vite、不打包）**，由 7801 直接托管一个 `admin/` 目录：
管理台就三个输入框 + 一个用户列表，React 是杀鸡牛刀；
"不是所有前端都该上框架"本身就是教学点（决策点 D3）。

### 3.6 PostgreSQL

- `docker run -d --name routeplanner-pg -e POSTGRES_PASSWORD=... -p 5432:5432 postgres:16-alpine`
- `go.mod`：`gorm.io/driver/postgres` 替换 `gorm.io/driver/mysql`——**GORM 的红利**：
  repo 层代码几乎零改动（`Tasks` 表定义不用动），这正是当初"上 ORM 不手写 SQL"的兑现
- `.env` 新增：`PG_DSN=host=localhost port=5432 user=... password=... dbname=routeplanner sslmode=disable`
- 没配 `PG_DSN` 时的行为：沿用现有装配哲学——异步 `/plans` 不注册，同步功能照常（决策点 D5）

## 4. 新增/改动文件清单

```
internal/auth/auth.go        # users/sessions/bcrypt/中间件(一个包收口,和 planner 同哲学)
internal/settings/provider.go# KeyProvider:DB>env+atomic 缓存
internal/amap/amap.go        # key 从 Provider 取(小改)
internal/api/router.go       # 用户端挂 /auth/* /config/public;拆出 NewAdminRouter
main.go                      # 两个 engine + 两个 http.Server;admin 种子
repo.go / go.mod             # GORM driver 换 postgres
frontend/                    # 登录/注册入口 + useSettings 三层优先级
admin/                       # 管理页(原生 HTML,Go 托管)
docker-compose.yml           # postgres 容器(顺手把 mysql 注释退役)
```

## 5. 实施分期（每期独立可验证，TDD）

| 期 | 内容 | 验证 |
|---|---|---|
| **P1 换库** | PG 容器 + GORM driver 换 + `.env` PG_DSN | go test 全绿；真 PG 起来后 /plans 异步链路跑通 |
| **P2 认证** | auth 包：注册/登录/登出/bcrypt/会话 + 中间件 | TDD：错密码 401、过期会话 401、cookie 属性断言 |
| **P3 用户端接线** | 前端登录页/用户角标；`/config/public` 下发 JS key；useSettings 三层优先级 | agent-browser 实测登录态 + 地图仍渲染 |
| **P4 Key 中心化** | settings Provider + amap.Client 动态 key + 热生效 | TDD：DB 优先/空值回落 env/改 key 不重启生效 |
| **P5 管理端** | 7801 引擎 + admin API + 原生管理页 + RequireAdmin | TDD：非 admin 403；浏览器实测改 key 后 /plan 用新 key |
| **P6 收尾** | smoke 增登录用例；README；MySQL 容器退役说明 | smoke 全绿 |

## 6. 安全红线（写进代码注释的）

1. REST key 只存在于服务端，任何接口不得下发；
2. 密码只存 bcrypt hash；登录失败**不区分**"用户不存在"和"密码错"（防枚举）；
3. 会话 token 用 `crypto/rand` 32B，不拿用户信息拼；
4. 管理端每条路由过 `RequireAdmin`（cookie 跨端口的教训，见 §3.1）；
5. 管理页回显 key 一律掩码（`4088****557`），防止肩窥/截图泄露。

## 7. 决策点（用户定）

- **D1 用户端要不要强制登录**：推荐**游客可用**（规划是公开体验,登录为将来"历史记录"铺路）；
  要强制也可以,但首屏就弹登录会赶走人。
- **D2 会话方案**：推荐**DB 会话 + HttpOnly cookie**；JWT 留作将来微服务拆分时再换（Phase 3 呼应）。
- **D3 管理页形态**：推荐**原生 HTML 单页**（Go 直接托管）；想练 React 路由也可以做成同一 SPA 的 /admin。
- **D4 key 热生效**：推荐**热生效**（atomic 缓存）；重启生效实现省一半但体验差。
- **D5 没配 PG_DSN 时**：推荐沿用装配哲学——同步功能照常、auth/异步不注册（依赖可空一贯如此）。
- **D6 MySQL 容器**：直接停用退役（tasks 教学数据不迁移），还是留着对照？推荐退役。
- **D7 admin 引导**：推荐 env 种子（`ADMIN_USER/ADMIN_PASSWORD`）；也可以首次启动打印随机密码到日志。

## 8. 验收标准

1. `go test ./... -race` 全绿，auth/settings 各有独立测试（含降级/回退路径）；
2. 浏览器实测走查：注册→登录→规划（用户端）→ 7801 登录同一账号 → 改 key →
   不重启服务，用户端下一次 /plan 用新 key → 清除 key → 回落 env；
3. REST key 在 HTTP 响应/前端代码中 grep 不到；
4. smoke 全绿；PG 容器自建可复现（README 有命令）。
