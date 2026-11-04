# 项目指南（AGENTS.md）

> 本文件由 AI 编码助手自动加载，是本工作区跨会话的持久项目记忆。

## 项目概述

多点路线自动规划系统（Route Planner）：用户输入起点和若干想去的地点，系统算出最优游览顺序并给出方案与真实路网轨迹。

- 后端：Go + gin，端口 **7800**（管理端 **7801**）
- 前端：React 19 + Vite，源码 `frontend/`，产物 `web/`（gin NoRoute 托管，站点根路径 `/`）
- 数据库：MySQL 8（Docker `routeplanner-mysql`，3306）+ gorm；Redis 7.2.5（6379）缓存 + Stream
- 模块：规划（/plan /route /search，异步 /plans + Stream worker）、认证（/auth）、社区 Feed（/community）、管理端

规格与实施计划在 `docs/superpowers/specs/`、`docs/superpowers/plans/`。

## 关键决策（已确认，不要改）

- **数据库 = MySQL 8 是唯一事实源**（2026-09-27 从 PG 切回，旧 PG 容器/卷/镜像已删）。
  社区表必须与 users 同库（JOIN/事务）
- 地理底座：距离默认 **Valhalla 自托管**（`DISTANCE_SOURCE=valhalla`，零 key）；搜索默认 **Photon**（`SEARCH_SOURCE=photon`）。统一接口 `internal/geo.Source`；amap 遗留门控 `DISTANCE_SOURCE=amap` 可切回，amap 包与测试原样保留
- 大陆网络实测：公共 Nominatim 超时、tile.openstreetmap.org 不可达、CARTO 要 key；
  **默认瓦片源 = tile.openstreetmap.de**，别"改回官方"（会复现不可达）
- 手写 TSP（最近邻 → 2-opt → 模拟退火），不引 OR-Tools（cgo 太重）；Redis Streams 做队列，不引 Kafka
- 坐标全链路 WGS-84；前端地图 Leaflet + OSM（`useLeaflet`）
- 高德 key 按「服务平台」分：前端地图要 **Web端(JS API)**，后端 REST 要 **Web服务**，同一把 key 可追加多平台。
  报错码：10001=key 不存在；10009=平台不对；`INVALID_USER_SCODE`=缺安全密钥（2021-12-02 后 key 的 JS API 2.0 强制，纯渲染不需要、调接口才需要）。
  **`AMAP` 就是高德（AutoNavi）**，`AMAP_KEY` 只是环境变量名，不是第二厂商
- key 存 DB `app_settings`（amap_rest_key / amap_js_key），`/config/public` 下发前端，`.env` 降为兜底

## 后端约定

- 校验收口 api 层；白名单统一走 `ParseMode()` 一类函数，`/plan` 与 `/route` 同一套标准
- **非法参数 → 400 绝不兜底**；参数错优先于依赖不可用
- 降级必须可上报（走返回值到 HTTP，不能只写日志）；`is_degraded` 单一真相源 = `len(warnings)>0`；
  没配 `AMAP_KEY` 走 haversine 是"设计如此"，不算降级、不报警告
- 上限按最坏路径：`MaxPoints=50`（驾车批量）/ `MaxPointsPairwise=10`（逐对/混合，350ms 节流）
- 高德调用一律 `c.url(pathXxx)`，路径常量化，根地址是 Client 字段，别引包级可变状态；外部调用必须设超时（5s）
- `order` 给人看（名字）、`order_idx` 给机器（下标）——名字不是标识符
- MySQL DSN 三参数缺一不可：`parseTime=True` + `charset=utf8mb4` + `loc=Local`；
  唯一键冲突判定用 `errors.As(*mysql.MySQLError)` 判 1062，别用报错文案字符串匹配
- GORM 软删自动过滤只在挂 `Model()` 时生效；`Table()+JOIN+自定义列` 查询必须手写 `deleted_at IS NULL`
- **新增实体必须加进 AutoMigrate 列表**（内存 fake 不建表，这类错误只有真库验证能抓到）
- Redis 只在 `AMAP_KEY` 非空时装配；日志 `redis cache enabled` = 真缓存，`redis unavailable` = 真降级
- `.env` 多词值必须加引号（MySQL DSN 含 `&`/`()` 同样必须单引号包裹）；重复键后者覆盖前者

## 前端约定

- `vite.config.js`：`base:'/'` + `outDir:'../web'` **必须同指一处**，只改一个就 404
- 开发：`npm run dev`（5173 + HMR，后端须跑 7800）；构建：`npm run build`。
  产物带内容 hash，无需手动 bump 版本。`web/` 整目录 gitignore，别往里加手写文件
- 设计变量单一来源：`tokens.css`（CSS）+ `theme.js`（JS，地图折线读不到 CSS 变量），两处同步
- 颜色：墨 `#1D1D1F` 唯一交互色；蓝 `#0066CC` 只给驾车；绿=步行；橙=公交。
  默认地图视野北京（`DEFAULT_CENTER=[116.397,39.909]`，高德经度在前）
- `legMode` 挂在**点上**（点到下一点怎么走），别退回 points+legs 平行数组
- 拖拽用原生 HTML5 DnD（跨事件传值放 `useRef`），不引 SortableJS
- **React 坑**：地图 hook 用 callback ref + state，别用 useRef + effect([])——容器晚于 effect 挂载时永不初始化；
  JSX `{}` 天然转义不需 esc()；前端校验只是体验优化，后端那份一个都不能少
- 视图切换无 router：App 顶层 `view` 状态决定渲染子树（与登录墙同一模式）

## RN App 约定（app/，2026-11-02 起）

- RN 0.88 bare + React 19.3 + TS；`src/api/types.ts` 是后端 wire contract 的 TS 镜像，
  改后端 json tag 必须同步它（双向测试钉住）；构建指南 `docs/rn-app.md`
- 地图 = WebView 内嵌 Leaflet + postMessage 桥（`src/map/bridge.ts` 是唯一通信契约，
  改协议两侧同步）；WebView 侧内容不可信，解码失败一律返回 null 不抛错
- `legMode` 同样挂在点上；segments 去重收在 `usePlan`（全段=全局 mode 时不传，
  保住后端驾车批量快路径），调用方不用各自判断
- hooks 的 api 参数**别用默认参数** `api = createApiClient(...)`——每渲染新建实例导致
  effect 无限重跑；用 `useRef` 持有（useSearch 有现成写法）
- RNTL v13：hook 测试必须在 act 内 `await` 异步动作（同步 act 不再即时提交）；
  mock 断言用当用例持有的 api 实例，别用 `mockApi.mock.results[0]`（跨用例污染）
- 验证三件套：`npx tsc --noEmit` + `npx jest` + Metro bundle（ios/android）；
  本机无 Xcode/Android SDK，原生构建只能用户侧做
- npm 装依赖需：全新缓存目录 + `env -u NODE_OPTIONS`（去注入 shim）+ 非沙箱；
  半成品 node_modules 用 `mv` 挪走（rename 不触发删除拦截），别直接 rm

## 测试约定

- 测试要能单独弄坏一个外部接口（`fakeAmapServer(t, failPaths...)`）才测得出降级
- 测 HTTP 契约时重写解码结构、路径写字面量，别复用内部类型
- 加修复就加一条会红的测试，并验证有牙齿（退回修复看它红不红）
- 关键改动跑 `bash scripts/smoke.sh`（需真 key；amap 模式专用，valhalla 模式跳过属预期）
- `scripts/smoke.sh` 广州三点基准（驾车 ≈45km vs 直线 ≈29km）是实测值，别改坐标

## 环境与常用命令

- Go 沙箱：`export PATH="/opt/homebrew/bin:$PATH" GOPATH=$PWD/.gopath GOCACHE=$PWD/.gocache GOMODCACHE=$PWD/.gomodcache`
- MySQL：`docker start routeplanner-mysql`（compose：`docker compose up -d mysql`）
- Redis：`.redis-src/redis-7.2.5/src/redis-server --port 6379 --bind 127.0.0.1 ::1 --save '' --appendonly no`
- 起服务：`export PATH=… && set -a && source .env && set +a && export GOPATH=… && PORT=7800 go run .`
- 停服务：`kill $(lsof -nP -iTCP:7800 -sTCP:LISTEN -t)`
- 用户常用工作流：`go build -o /tmp/rp-final` 自己跑二进制占 7800。**代管前先 `lsof` 看是不是他的 rp-final，别杀**
- 长跑进程用持久后台任务，`&`/`nohup` 会随会话被带走
- 本会话 shell 若有 HTTP_PROXY，curl 本机要 `--noproxy '*'`
- macOS 坑：BSD `grep` 不支持 `\|`（用 `grep -E`）；BSD `xargs` 无 `-r`；`sed -i` 需跟后缀参数

## 代码风格

- 接收者 `c/s/r`、`i/j/k/err/ok/cfg` 是 Go 惯例；作用域越远，名字越描述
- 代码注释保持专业简洁，只留：设计决策、非显而易见的坑、契约约束
- 对外文档（README 等）：标准技术文档风格，客观描述功能与设计，不堆 emoji、不写口语化内容
- gofmt 已知不合规 5 文件（Go1.19 doc comment 缩进差异）：main.go、amap.go、amap_test.go、model.go、worker.go；只查项目自身：`gofmt -l main.go internal/`
- 代码改动后同步知识图谱：跑 `/understand`（增量模式），再 `python3 .understand-anything/build-dashboard.py` 刷新 dashboard
