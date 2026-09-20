// api.js —— 后端接口的唯一出口
//
// 所有请求统一收口在这里:错误归一化、resp.ok 判断、错误消息提取只写一份。
// "怎么发请求"与"界面长什么样"分离,组件只关心数据,不关心 HTTP 细节。
//
// 所有请求都用相对路径('/plan' 而不是 'http://localhost:7800/plan'):
// 页面由 Go 托管、天然同源,前端不感知后端端口;
// 后期拆 gRPC 微服务、上反向代理、换域名,这里都不用改。

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
 * 401 有专门的通道：session 过期时（登录墙启用后业务接口会 401），
 * 派发全局事件 auth:expired —— useAuth 监听它把 user 置空，
 * App 的登录墙自动接管。业务组件不用各自处理"过期了怎么办"。
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

  if (resp.status === 401) {
    // 只在"主界面已挂载"时派发才有意义:登录页上的 /auth/me 401 是正常探测,
    // 那时 user 本来就是 null,派发了也无人响应(useAuth 里 user 已是 null,幂等)
    window.dispatchEvent(new Event('auth:expired'))
    const msg = data?.error || '登录已过期，请重新登录'
    throw new Error(withHint(msg))
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

// ── 认证与公开配置（会话靠 HttpOnly cookie，前端不碰任何 token）──

/** 注册新用户。后端注册成功会顺带登录（发会话 cookie）。 */
export function registerUser(username, password) {
  return request('/auth/register', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
}

/** 登录。凭据错抛 401 的中文错误。 */
export function loginUser(username, password) {
  return request('/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
}

/** 登出：后端删服务端会话 + 清 cookie。 */
export function logoutUser() {
  return request('/auth/logout', { method: 'POST' })
}

/** 当前登录用户。未登录抛错（401）。 */
export async function fetchMe() {
  const data = await request('/auth/me')
  return data.user
}

/**
 * 拉取管理员配置的 JS key / 安全密钥（公开接口，JS key 本来就暴露在浏览器里）。
 * 用于「localStorage 没有个人覆盖」时的默认值 —— 三层优先级的中间一层。
 * 拉不到（旧版后端 / 网络问题）返回 null，调用方静默回退到空态。
 */
export async function fetchPublicConfig() {
  try {
    const data = await request('/config/public')
    return data
  } catch {
    return null
  }
}
