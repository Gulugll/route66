// api.js —— 后端接口的唯一出口
//
// 手写版里 fetch 散在三处（searchPlaces / plan / drawRoute），
// 每处都要自己 resp.json()、自己判断 resp.ok、自己从 data.error 里取消息 ——
// 三份几乎一样的代码，改一处忘两处。
//
// 这里统一收口成三个函数。教学点：**把"怎么发请求"和"界面长什么样"分开**，
// 组件里只关心数据，不关心 HTTP 细节。这也叫"关注点分离"。
//
// 另一个细节：所有请求都用**相对路径**（'/plan' 而不是 'http://localhost:7800/plan'）。
// 因为页面由 Go 托管，天然同源，前端不该知道后端端口 ——
// 后期拆 gRPC 微服务、上反向代理、换域名，这里都不用改。

/**
 * 给后端的错误补上"该去哪儿修"。
 *
 * 后端报的是「未配置 AMAP_KEY,搜索不可用」—— 它诚实，但用户看完不知道下一步做什么。
 * 错误信息能被"行动"才算完整，所以在这里补一句。
 *
 * 放在 api.js 而不是组件里，是因为这个文件本来就是"错误归一化"的唯一出口
 * （见文件头），补一次，search / plan / route 三条路径全都受益。
 */
const ERROR_HINTS = [
  [
    '未配置 AMAP_KEY',
    '后端要的是「Web服务」平台的高德 key（服务端环境变量 AMAP_KEY），不是浏览器里那把 JS key；见 README 的「高德 key」一节',
  ],
]

function withHint(msg) {
  const hit = ERROR_HINTS.find(([frag]) => msg.includes(frag))
  return hit ? `${msg} —— ${hit[1]}` : msg
}

/**
 * 统一的请求封装：负责 HTTP 细节 + 错误归一化。
 * 失败时抛 Error，message 就是可以直接给用户看的中文提示。
 *
 * @param {string} path 相对路径，如 '/plan'
 * @param {RequestInit} [options]
 */
async function request(path, options) {
  const resp = await fetch(path, options)

  // 后端约定：出错时返回 { error: "中文说明" }（见 internal/api/router.go）。
  // 但 502/503 这类可能是网关返回的、格式不保证，所以解析失败要兜底 ——
  // 不能因为"错误响应不是 JSON"就让前端自己崩掉。
  let data = null
  try {
    data = await resp.json()
  } catch {
    data = null
  }

  if (!resp.ok) {
    const msg = data?.error || `请求失败（HTTP ${resp.status}）`
    throw new Error(withHint(msg))
  }
  return data
}

/** 搜索地点。city 为空 = 全国搜索。 */
export function searchPlaces(q, city) {
  const params = new URLSearchParams({ q })
  if (city) params.set('city', city)
  return request('/search?' + params.toString())
}

/**
 * 规划路线。
 *
 * @param {{points: Array}} args
 *   points[0] 是起点，其余是目的地。manual=true 时按列表顺序、逐段用各自的方式算；
 *   manual=false 时忽略 segments，交给后端跑模拟退火求最优顺序。
 *
 * 注意 manual=false 时**不要**发 segments 字段 ——
 * 后端是"看到 segments 非空就走混合出行分支"的（见 router.go 的 len(req.Segments) > 0），
 * 多发一个空数组都可能改变它走哪条路径。接口约定要精确，不能"多传点也无所谓"。
 */
export function planRoute({ points, manual }) {
  const [origin, ...destinations] = points
  const body = {
    origin,
    destinations,
    manual,
  }
  if (manual) {
    // 每一段的出行方式。长度必然是点数-1，后端会校验这一点。
    body.segments = points.slice(0, -1).map((p) => p.legMode)
  }
  return request('/plan', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

/**
 * 取两点之间的真实路网轨迹。
 * 返回 [lng, lat] 数组；公交可能返回空数组（高德就不给公交轨迹）。
 */
export async function fetchRoute(a, b, mode) {
  const params = new URLSearchParams({
    origin: `${a.lng},${a.lat}`,
    dest: `${b.lng},${b.lat}`,
    mode,
  })
  const data = await request('/route?' + params.toString())
  return data.polyline || []
}
