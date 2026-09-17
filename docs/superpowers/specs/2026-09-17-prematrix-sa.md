# 方案：直线预矩阵解序 + 沿最终顺序查真实路网（v2，取代聚类方案）

> 2026-09-17 · 取代同日前稿（cluster-first, route-second，用户判定会割裂路线，已搁置——
> 思路仍记录在会话记忆里，将来点数上千可再捡起来）。
> 状态：**已实现（2026-09-17）**——go test ./... -race 全绿；变异验证通过
> （退回全矩阵 → 计数测试抓到 12 次 vs 期望 3 次）；smoke 12/12。
> 实施中的两笔顺带修复：① worker_test 的 fakeRepo 无锁，-race 抓到
> 测试工具竞态，已加 sync.Mutex；② smoke 驾车基准 40~50 → 40~70：
> 高德数据漂移（实测 白云山→长隆 从 ~28km 涨到 43.8km，总距离 43.51→59.24），
> 与本次代码无关，下界 40 仍能识破降级到直线（29km）的假象。
> 顺带发现的算法性质（**已修，见 solver/tsp.go 2026-09-17**）：3 点开放路径下旧邻域
> 为空（SA 退化纯 NN），且"边界两条边"判据在不对称矩阵上会误拒最优移动 ——
> 现邻域允许反转段贴到末端 + 精确 TourLength 差评估，
> TestAsymmetricThreePointTrap 用当天实测数据钉住（59.240 → 44.909），
> smoke 驾车总距离同日从 59.24 回到 44.909。

## 1. 一句话

**解序用免费的直线距离，算账和画线才花钱**——SA 在 haversine 预矩阵上跑（0 API），
顺序定了之后，只对最终顺序的 n-1 条相邻边发高德请求。

## 2. 为什么值得改（按模式算账）

| 模式 | 现状（建全矩阵） | 改后 | 省 |
|---|---|---|---|
| walking / transit | n×(n-1) 对，350ms 节流，n=10 即 90 次 ≈ 32s | n-1 次 ≈ 3s | **~90%** |
| driving | n 次（批量逐列，4 路并发）≈ 1s | **保持现状**（见 D1） | — |
| manual（手动模式） | 也在建**全量矩阵**！但只用了相邻 n-1 条 | n-1 次 | 纯浪费，直接砍掉 |

手动模式是现状里最冤的一个：根本不重排顺序，却为 `TourLength` 建了整张矩阵。
直线预矩阵 + SA 这条路对 manual 同样成立（连 SA 都不用跑）。

## 3. 设计

### 3.1 `planner.Compute` 新流程（单一方式分支）

```
mode == driving（且 am != nil）→ 走现有路径：m.DistanceMatrix 真实矩阵 → SA/顺序 → TourLength
其他情况：
  1) dists = haversineMatrix(points)        // 本地纯公式，0 API（matrix 包已有，需导出）
  2) order = manual ? seqOrder(n) : SimulatedAnnealing(dists, 0)
  3) for i in 0..n-2:
       km = am.Distance(points[order[i]], points[order[i+1]], mode)   // 带缓存+350ms 节流
       失败 → km = haversine(该对)，warnings/degraded 记该段（与混合出行同一套降级范式）
  4) TotalKm = Σ km
am == nil（没配 key）→ 跳过第 3 步，TotalKm = 直线距离和。
  按既有约定这是"设计如此"，不算降级、不报警告。
```

### 3.2 质量取舍（写进注释的实话）

- 直线远近 ≠ 实走远近（单行道/高架/绕路），解序质量会略降。
  步行/公交场景两者高度相关，损失可接受；
  **驾车损失最明显且批量矩阵本来就便宜**——所以推荐驾车不切换（D1）。
- 降级粒度从"整张矩阵"变成"某一条边"：`is_degraded` 语义不变（`len(warnings)>0`），
  `degraded` 数组从空/整线变成具体路段名（和混合出行分支对齐，前端展示逻辑复用）。

### 3.3 涉及的代码点

- `internal/planner/planner.go`：`Compute` 单一方式分支重写（§3.1）；
  `MaxPointsPairwise` 的注释更新（限制动机从"建矩阵太慢"变成"逐段查路仍慢"，n=10 时 9 次 ≈ 3s，上限可放宽——但本轮不动数值，只改注释）
- `internal/matrix/matrix.go`：导出 `HaversineMatrix(points)`（现在 `haversineMatrix` 是私有的）
- **不动**：`solver` 包（SA 原样复用，它不关心矩阵哪来的）、api 层、前端、worker

## 4. 实施步骤（TDD，每步先红后绿）

| # | 步骤 | 测试（先写，会红） |
|---|---|---|
| 1 | 导出 `matrix.HaversineMatrix` | 已有行为，编译级验证即可 |
| 2 | manual 模式走"n-1 条边" | **请求计数断言**：fakeAmapServer 统计收到的距离请求数，n=4 点 manual 断言**恰好 3 次**（现状是 12 次） |
| 3 | walking 自动模式走预矩阵+SA+逐边 | 同上计数：n=4 断言 3 次；且顺序正确（SA 在直线矩阵上解出）；断言 total_km 来自真实路网距离而非直线 |
| 4 | 逐边降级 | `fakeAmapServer(t, failPath)` 弄坏第 2 条边：断言 total_km = 前后真实+中间直线，`degraded` 含该路段名，`is_degraded=true` |
| 5 | 没配 key | am=nil：不报警告（设计如此），total_km 全直线 |
| 6 | driving 不回归 | 现有 driving 测试全部照常通过（证明 D1 路径没被误改） |
| 7 | `go test ./... -race` + `bash scripts/smoke.sh`（需真 key） | smoke 12/12，基准 43.51km 不变（driving 路径未动） |

**变异验证**：把第 2/3 步实现退回"建全矩阵"，计数测试必须变红——证明测试真有牙齿。
这是本方案最有教学价值的一招：**fakeAmapServer 从"能不能成功"升级成"数你发了几次请求"**。

## 5. 决策点（用户定）

- **D1 驾车要不要也切换**：推荐**不切**——批量矩阵 n 次请求本来就便宜，真实路网矩阵的解序质量还更高。
  切了反而变慢（逐边 350ms 节流，n=50 要 24s+）。
- **D2 manual 是否同时改**：推荐**改**（纯收益，不用 SA 逻辑，顺手砍掉全矩阵浪费）。
- **D3 walking/transit 自动模式是否本轮就上**：推荐**是**（这是本方案 90% 的收益来源）。
- **D4 `MaxPointsPairwise` 上限要不要放宽**：本轮只改注释，数值先不动（改动越少越好验）。

## 6. 验收标准

1. `go test ./... -race` 全绿，新增测试 ≥ 4 条（含请求计数断言）；
2. n=10 walking 手动规划：高德请求数从 90 → 9（smoke 或手动 curl 验证）；
3. 降级行为与混合出行分支的表现一致（路段级 degraded + is_degraded）；
4. driving 路径零改动，smoke 12/12；
5. 讲解覆盖：为什么解序可以用直线、质量损失在哪、请求计数测试怎么写。
