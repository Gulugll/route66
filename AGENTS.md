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
- **前端先行、前后端分离**：页面是独立 HTML/JS，gin NoRoute 托管 web/，与 API 同源无 CORS
- **路线绘制：目前前端画直线**（匹配 haversine 直线距离），以后接高德再升级真实路网 polyline
- **开源：key 由使用者自配**——前端 JS key 存浏览器 localStorage（设置面板可换，进 settingsOverlay）；后端将来用的高德 Web 服务 key 走环境变量 AMAP_KEY

## 进度追踪

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

1. **状态**：Phase 1 MVP 功能全部完成——搜索（后端代理+全国可选城市）、多出行方式（驾车/步行/公交）、混合出行（每段独立方式）、拖拽排序（SortableJS 本地化+索引 bug 已修）、彩色分段路线+图例、Redis 缓存。用户最后确认拖拽正常（`v=20260820i`）
2. **晚上可选项**：① 用户浏览器再实测一轮，收尾 Phase 1；② **Phase 2（Redis Stream 异步 + MySQL 持久化）**——Redis 环境已就绪（`.redis-src/redis-7.2.5/src/redis-server`）；③ 已知小项：模拟退火固定随机种子
3. 启动命令：`AMAP_KEY=<Web服务key>` + `.redis-src/redis-7.2.5/src/redis-server --port 6379 --save '' --appendonly no &` + `/tmp/routeplanner`（需先 `go build`，带 GOPATH/GOCACHE/GOMODCACHE env）
4. 备忘：Go 命令带 `GOPATH/GOCACHE/GOMODCACHE` 三个 env（指工作区）；前端改动后 bump `app.js?v=` 版本号

---

**维护规则**：每次教完一个知识点，更新"进度追踪/下次继续"。更新本文件时，同步考虑是否回写 Claude 记忆 `~/.claude/projects/-Users-shoushenwei-GolandProjects-awesomeProject/memory/teaching-plan.md`，保持两套记忆一致。
