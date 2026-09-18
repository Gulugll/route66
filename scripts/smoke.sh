#!/usr/bin/env bash
# 冒烟测试：验证「真实路网 + 缓存 + 三个接口」是否真的工作。
#
# 为什么需要它：单元测试用 httptest 模拟高德，抓不到"真实 API 行为"这类问题
# （比如 v3/distance 的批量语义是"多起点→单终点"、步行接口没轨迹）。
# 所以外部 API 集成必须做一次真 key 冒烟 —— 这个脚本就是那一次。
#
# 用法（服务必须先启动，且带了 AMAP_KEY）：
#   set -a; source .env; set +a; ./awesomeProject &
#   bash scripts/smoke.sh
#
# 判定「降级」的技巧：3 个固定点驾车，真实路网 ≈ 45 km，haversine 直线 ≈ 29 km。
# 两者差得远，所以用区间断言就能自动识别出"其实降级了但接口返回 200"的情况。
set -u

BASE=${BASE:-http://localhost:7800}
ADMIN_BASE=${ADMIN_BASE:-http://localhost:7801}
REDIS_CLI=${REDIS_CLI:-.redis-src/redis-7.2.5/src/redis-cli}
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0

ok()   { echo "  ✅ $1"; pass=$((pass+1)); }
bad()  { echo "  ❌ $1"; fail=$((fail+1)); }
# between 描述 实际值 下界 上界
between() {
  awk -v v="$2" -v lo="$3" -v hi="$4" \
    'BEGIN { if (v+0 >= lo && v+0 <= hi) exit 0; exit 1 }' \
    && ok "$1 = $2（落在 $3 ~ $4）" || bad "$1 = $2（期望 $3 ~ $4）"
}

PLAN_BODY='{"origin":{"name":"广州塔","lat":23.1066,"lng":113.3245},
"destinations":[{"name":"白云山","lat":23.1794,"lng":113.2956},{"name":"长隆","lat":22.9976,"lng":113.3275}],
"manual":false}'

echo "== 0. 服务与依赖 =="
code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")
[ "$code" = "200" ] && ok "/healthz 200" || bad "/healthz 返回 $code"
if $REDIS_CLI -p 6379 ping >/dev/null 2>&1; then
  ok "Redis 存活（PONG）"
else
  bad "Redis 不通 —— 缓存会降级到进程内 Memory"
fi

echo
echo "== 1. 缓存清空（保证第一次请求真的打高德）=="
# 注意用 while read 而不是 xargs：macOS 的 BSD xargs 不支持 GNU 的 -r 选项
$REDIS_CLI -p 6379 --scan --pattern 'dist:*' 2>/dev/null | while read -r k; do
  $REDIS_CLI -p 6379 DEL "$k" >/dev/null 2>&1
done
echo "  已删除 dist:* 旧 key"

echo
echo "== 2. 首次 /plan（驾车，应打真实路网并写缓存）=="
t1=$(curl -s -o "$TMP/plan1.json" -w '%{time_total}' -X POST "$BASE/plan" \
     -H 'Content-Type: application/json' -d "$PLAN_BODY")
km1=$(grep -o '"total_km":[0-9.]*' "$TMP/plan1.json" | cut -d: -f2)
echo "  原始响应: $(cat "$TMP/plan1.json")"
echo "  耗时: ${t1}s"
# 区间下界 40 是"识破假降级"的关键:真实路网约 45km+,直线只有 29km。
# 上界故意放宽到 70:高德路网数据会漂移(2026-09-17 实测 白云山→长隆
# 从 ~28km 涨到 43.8km,总距离 43.51→59.24),区间要容得下这种外部变化,
# 但仍要能把"悄悄降级成直线"抓出来。
between "首次 total_km（真实路网）" "$km1" 40 70
if grep -q '"is_degraded":true' "$TMP/plan1.json"; then
  bad "发生了降级（is_degraded=true）—— 检查服务日志里的 [matrix]/[plan] 行"
else
  ok "未降级（真实路网距离）"
fi

echo
echo "== 3. 缓存是否写入 =="
keys=$($REDIS_CLI -p 6379 --scan --pattern 'dist:*' 2>/dev/null | wc -l | tr -d ' ')
[ "$keys" -ge 6 ] && ok "dist:* key 数 = ${keys}（3 点应有 6 个有向对）" \
                  || bad "dist:* key 数 = ${keys}（少于 6，检查缓存写入）"
ttl=$($REDIS_CLI -p 6379 TTL "$($REDIS_CLI -p 6379 --scan --pattern 'dist:*' 2>/dev/null | head -1)" 2>/dev/null | tr -d ' ')
echo "  抽样 TTL = ${ttl}s（应接近 86400）"

echo
echo "== 4. 二次 /plan（同参数，应零高德请求）=="
t2=$(curl -s -o "$TMP/plan2.json" -w '%{time_total}' -X POST "$BASE/plan" \
     -H 'Content-Type: application/json' -d "$PLAN_BODY")
echo "  耗时: ${t2}s"
awk -v a="$t1" -v b="$t2" 'BEGIN { exit !(b+0 <= a+0) }' \
  && ok "二次不比首次慢（缓存命中）" \
  || bad "二次(${t2}s) 比首次(${t1}s) 慢 —— 缓存可能没生效"

echo
echo "== 5. /search（后端代理高德搜索）=="
curl -s -o "$TMP/search.json" "$BASE/search?q=%E5%B9%BF%E5%B7%9E%E5%A1%94"
n=$(grep -o '"name":' "$TMP/search.json" | wc -l | tr -d ' ')
[ "$n" -ge 1 ] && ok "搜索「广州塔」返回 $n 条候选" || bad "搜索返回 0 条：$(cat "$TMP/search.json")"

echo
echo "== 6. /route（真实路网轨迹，前端画线用）=="
curl -s -o "$TMP/route.json" "$BASE/route?origin=113.3245,23.1066&dest=113.2956,23.1794&mode=driving"
pts=$(grep -o '\[' "$TMP/route.json" | wc -l | tr -d ' ')
[ "$pts" -ge 20 ] && ok "驾车轨迹点数 = ${pts}（远超 2 点，是真轨迹不是直线）" \
                  || bad "轨迹点数 = ${pts}（疑似空轨迹，前端会降级画直线）"

echo
echo "== 7. 混合出行（每段不同方式）=="
for mode in walking transit; do
  body='{"origin":{"name":"广州塔","lat":23.1066,"lng":113.3245},
  "destinations":[{"name":"白云山","lat":23.1794,"lng":113.2956}],
  "manual":true,"segments":["'"$mode"'"]}'
  out=$(curl -s -X POST "$BASE/plan" -H 'Content-Type: application/json' -d "$body")
  km=$(echo "$out" | grep -o '"total_km":[0-9.]*' | cut -d: -f2)
  if [ -n "$km" ]; then
    if echo "$out" | grep -q '"is_degraded":true'; then
      bad "$mode 段降级了：$out"
    else
      ok "$mode 单段 total_km = $km"
    fi
  else
    bad "$mode 请求失败：$out"
  fi
done

echo
echo "== 8. 参数校验（防御性编程是否真的拦得住）=="
c1=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/plan" -H 'Content-Type: application/json' \
     -d '{"origin":{"name":"a","lat":999,"lng":113},"destinations":[{"name":"b","lat":23,"lng":113}]}')
[ "$c1" = "400" ] && ok "非法坐标 → 400" || bad "非法坐标 → ${c1}（期望 400）"
c2=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/plan" -H 'Content-Type: application/json' \
     -d '{"origin":{"name":"a","lat":23,"lng":113},"destinations":[{"name":"b","lat":23,"lng":113},{"name":"c","lat":23,"lng":113}],"segments":["walking"]}')
[ "$c2" = "400" ] && ok "segments 长度不符 → 400" || bad "segments 长度不符 → ${c2}（期望 400）"

# ── 9. 登录与管理台(可选段:没配 PG/ADMIN_PORT 时整段跳过)──
# 判据:7801 的 /admin/healthz 通 = 管理端已启用,才做认证断言。
# 用户名带 $RANDOM:smoke 重跑不会撞 unique 约束。
adminUp=$(curl -s -o /dev/null -w '%{http_code}' "$ADMIN_BASE/admin/healthz" || true)
if [ "$adminUp" = "200" ]; then
  echo
  echo "== 9. 登录系统与管理台（可选段）=="
  u="smoke_$RANDOM"
  c3=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
       -d "{\"username\":\"$u\",\"password\":\"smoke-pass-66\"}")
  [ "$c3" = "200" ] && ok "注册 → 200" || bad "注册 → ${c3}（期望 200）"
  c4=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/auth/register" -H 'Content-Type: application/json' \
       -d "{\"username\":\"$u\",\"password\":\"smoke-pass-66\"}")
  [ "$c4" = "409" ] && ok "重名注册 → 409" || bad "重名注册 → ${c4}（期望 409）"
  c5=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/auth/me")
  [ "$c5" = "401" ] && ok "未登录 /auth/me → 401" || bad "/auth/me → ${c5}（期望 401）"
  c6=$(curl -s -o /dev/null -w '%{http_code}' "$ADMIN_BASE/admin/api/keys")
  [ "$c6" = "401" ] && ok "未登录管理 API → 401" || bad "管理 API → ${c6}（期望 401）"
else
  echo
  echo "== 9. 登录系统与管理台：跳过（7801 管理端未启用，属正常配置）=="
fi

echo
echo "======================================"
echo "  通过 $pass 项，失败 $fail 项"
echo "======================================"
echo "提示：服务日志里的 [amap] 行数 = 真实打出去的请求数。"
echo "     首次 /plan 应有 3 行（3 点逐列批量），二次应为 0 行。"
[ "$fail" -eq 0 ]
