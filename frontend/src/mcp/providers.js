// providers.js —— 三家地图 MCP 的接入配置与任务执行器。
//
// 坐标契约:本应用全链路 WGS-84;三家返回都是加密系(高德/腾讯 GCJ-02,百度 BD-09)。
// 出参进链路前一律转回 WGS-84,入参下发前转成厂商系。
//
// 形状来源:高德已用真实 key 实测(2026-11-05);腾讯按官方 WebServiceAPI 文档
// 防御性解析;百度 Web 端不可直连(端点无 CORS 头),仅保留注册表占位。
// 解析失败的段落一律降级(直线距离/空轨迹 + 警告),不让整体不可用 ——
// 与后端"某段失败退 haversine 并带警告"同一范式。

import { callTool, listToolNames } from './client.js'
import { getBinding } from './settings.js'
import { wgs84ToGcj02, gcj02ToWgs84 } from '../gcj02.js'

export const PROVIDERS = {
  amap: {
    label: '高德',
    transport: 'http',
    endpoint: (k) => `https://mcp.amap.com/mcp?key=${encodeURIComponent(k)}`,
    coord: 'lnglat', // 请求/响应都是"经度,纬度"
    web: true,
  },
  tencent: {
    label: '腾讯地图',
    transport: 'sse',
    endpoint: (k) => `https://mcp.map.qq.com/sse?key=${encodeURIComponent(k)}`,
    coord: 'latlng', // 腾讯 API 是"纬度,经度"
    web: true,
  },
  baidu: {
    label: '百度地图',
    transport: 'http',
    endpoint: (k) => `https://mcp.map.baidu.com/mcp?ak=${encodeURIComponent(k)}`,
    coord: 'lnglat',
    web: false, // 端点无 CORS 头,Web 端无法直连;仅 App 端原生请求可用
  },
}

// 各能力的工具名候选,按优先级与 tools/list 求交集解析
const ALIASES = {
  amap: {
    search: ['maps_text_search'],
    detail: ['maps_search_detail'],
    geo: ['maps_geo'],
    matrix: ['maps_distance'],
    route: {
      driving: ['maps_direction_driving'],
      walking: ['maps_direction_walking'],
      transit: ['maps_direction_transit_integrated'],
    },
  },
  tencent: {
    search: ['placeSuggestion', 'place_search'],
    detail: ['placeDetail'],
    geo: ['geocoder'],
    matrix: ['matrix', 'directionMatrix'],
    route: {
      driving: ['directionDriving'],
      walking: ['directionWalking'],
      transit: ['directionTransit'],
    },
  },
  baidu: {
    search: ['place_search', 'suggestion'],
    detail: ['place_detail'],
    geo: ['geocode'],
    matrix: ['distance_matrix', 'routmat'],
    route: {
      driving: ['direction_driving', 'routing'],
      walking: ['direction_walking'],
      transit: ['direction_transit'],
    },
  },
}

/** 工具名解析:capabilities 与 tools/list 实际名称求交,保证改名的厂商不至于直接哑火。 */
async function resolveTools(provider, key, kinds) {
  const names = await listToolNames({ endpoint: PROVIDERS[provider].endpoint(key), transport: PROVIDERS[provider].transport })
  const table = ALIASES[provider]
  const picked = {}
  for (const kind of kinds) {
    const cands = kind === 'route' ? null : table[kind]
    if (cands) {
      picked[kind] = cands.find((n) => names.includes(n)) || null
    }
  }
  return picked
}

function svcCoord(coord, p) {
  const [lng, lat] = wgs84ToGcj02(coord.lng, coord.lat)
  return p.coord === 'latlng' ? `${lat},${lng}` : `${lng},${lat}`
}

/** 加密系坐标 → WGS-84 [lng,lat](数组或 "x,y" 字符串都能吃)。 */
function toWgs(pair, p) {
  let lng, lat
  if (typeof pair === 'string') {
    const [a, b] = pair.split(',').map(Number)
    ;[lng, lat] = p.coord === 'latlng' ? [b, a] : [a, b]
  } else if (Array.isArray(pair)) {
    ;[lng, lat] = p.coord === 'latlng' ? [pair[1], pair[0]] : [pair[0], pair[1]]
  } else if (pair && typeof pair === 'object') {
    // {lat,lng} 或 {location:{lat,lng}}
    lng = pair.lng ?? pair.location?.lng
    lat = pair.lat ?? pair.location?.lat
  }
  if (!Number.isFinite(lng) || !Number.isFinite(lat)) return null
  return gcj02ToWgs84(lng, lat)
}

/** 两点直线距离(km)——降级兜底,与后端 matrix.Haversine 同式。 */
function haversineKm(a, b) {
  const R = 6371
  const rad = (d) => (d * Math.PI) / 180
  const dLat = rad(b.lat - a.lat)
  const dLng = rad(b.lng - a.lng)
  const h =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(rad(a.lat)) * Math.cos(rad(b.lat)) * Math.sin(dLng / 2) ** 2
  return 2 * R * Math.asin(Math.sqrt(h))
}

/** 在任意嵌套对象里找含坐标点串的字段(轨迹提取的兜底手段)。 */
function extractPolyline(obj) {
  if (!obj || typeof obj !== 'object') return []
  const direct = Array.isArray(obj) ? obj : null
  if (direct) {
    const pairs = direct
      .map((v) => (typeof v === 'string' ? v : null))
      .filter(Boolean)
    if (pairs.length && pairs[0].includes(',')) {
      return pairs.flatMap((s) => s.split(';'))
    }
    if (direct.length && Array.isArray(direct[0]) && direct[0].length === 2) {
      return direct.map((p) => `${p[0]},${p[1]}`)
    }
    return []
  }
  for (const key of ['polyline', 'coords', 'path', 'polyline_points']) {
    const v = obj[key]
    if (typeof v === 'string' && v.includes(',')) return v.split(';')
    if (Array.isArray(v) && v.length) return extractPolyline(v)
  }
  for (const v of Object.values(obj)) {
    if (v && typeof v === 'object') {
      const found = extractPolyline(v)
      if (found.length > 1) return found
    }
  }
  return []
}

// —— 三个执行阶段 ——

/** phase=search:关键词检索,归一化为 [{name,address,lat,lng}](WGS-84)。 */
async function execSearch(provider, key, params) {
  const p = PROVIDERS[provider]
  const tools = await resolveTools(provider, key, ['search', 'detail', 'geo'])
  const call = (tool, args) => callTool({ endpoint: p.endpoint(key), transport: p.transport }, tool, args)
  const limit = Math.min(params.limit || 5, 10)

  if (provider === 'amap') {
    // 实测:高德 text_search 的 POI 不带坐标,需逐个 detail 补
    const { data } = await call(tools.search, { keywords: params.keyword, city: params.city || '' })
    const pois = (data?.pois || []).slice(0, limit)
    const places = []
    for (const poi of pois) {
      try {
        const d = await call(tools.detail, { id: poi.id })
        const wgs = toWgs(d.data?.location, p)
        if (wgs) {
          places.push({ name: poi.name, address: poi.address || d.data?.address || '', lat: wgs[1], lng: wgs[0] })
        }
      } catch {
        // 单个 POI 失败跳过,不影响其余候选
      }
    }
    if (places.length) return places
    // 全部失败再试地理编码兜底(单点)
    const g = await call(tools.geo, { address: params.keyword, city: params.city || '' })
    const wgs = toWgs(g.data?.results?.[0]?.location, p)
    return wgs ? [{ name: params.keyword, address: g.data.results[0].district || '', lat: wgs[1], lng: wgs[0] }] : []
  }

  // 腾讯/百度:检索工具应直接带坐标,防御性归一化
  const { data } = await call(tools.search, {
    keyword: params.keyword,
    region: params.city || '',
  })
  const list = data?.data || data?.results || data?.pois || []
  const places = []
  for (const item of list.slice(0, limit)) {
    const wgs = toWgs(item.location || item, p)
    if (wgs) {
      places.push({
        name: item.title || item.name || params.keyword,
        address: item.address || '',
        lat: wgs[1],
        lng: wgs[0],
      })
    }
  }
  return places
}

/** phase=matrix:n×n 路网距离矩阵(km)。 */
async function execMatrix(provider, key, params) {
  const p = PROVIDERS[provider]
  const tools = await resolveTools(provider, key, ['matrix'])
  const call = (tool, args) => callTool({ endpoint: p.endpoint(key), transport: p.transport }, tool, args)
  const pts = params.points
  const n = pts.length
  const matrix = Array.from({ length: n }, () => Array(n).fill(0))
  const warnings = []
  let degraded = false

  if (provider === 'amap') {
    // maps_distance 支持多起点单终点:n-1 次调用拼全矩阵
    // type: 1=驾车 3=步行(公交无矩阵,后端不会进这一阶段)
    const type = params.mode === 'walking' ? '3' : '1'
    for (let j = 0; j < n; j += 1) {
      const originIdx = []
      const origins = []
      for (let i = 0; i < n; i += 1) {
        if (i !== j) {
          originIdx.push(i)
          origins.push(svcCoord(pts[i], p))
        }
      }
      try {
        const { data } = await call(tools.matrix, {
          origins: origins.join('|'),
          destination: svcCoord(pts[j], p),
          type,
        })
        for (const r of data?.results || []) {
          const i = originIdx[Number(r.origin_id) - 1]
          if (i === undefined) continue
          const km = Number(r.distance) / 1000
          if (Number.isFinite(km) && km > 0) {
            matrix[i][j] = km
          }
        }
      } catch (err) {
        // 整列失败:该列退直线,警告记到结果里
        for (let i = 0; i < n; i += 1) {
          if (i !== j) matrix[i][j] = haversineKm(pts[i], pts[j])
        }
        degraded = true
        warnings.push(`${pts[j].name} 的距离获取失败,相关路段已按直线估算`)
      }
    }
    return JSON.stringify({ matrix, degraded, warnings })
  }

  // 腾讯/百度:matrix 工具一次批量;入参/出参形状文档未逐字核实,失败整体退直线
  try {
    const fromArg = pts.map((c) => svcCoord(c, p)).join(';')
    const { data } = await call(tools.matrix, { from: fromArg, to: fromArg, mode: params.mode })
    // 防御性:找 results/rows/matrix 里的距离字段
    const rows = data?.result?.rows || data?.rows || data?.matrix || data?.results
    if (!Array.isArray(rows)) throw new Error('响应里没有矩阵')
    for (let i = 0; i < n && i < rows.length; i += 1) {
      const row = Array.isArray(rows[i]) ? rows[i] : rows[i]?.distance
        ? [{ distance: rows[i].distance }]
        : rows[i]?.elements || []
      for (let j = 0; j < n && j < row.length; j += 1) {
        const m = Number(row[j]?.distance)
        matrix[i][j] = Number.isFinite(m) && m > 0 ? m / 1000 : haversineKm(pts[i], pts[j])
      }
    }
    return JSON.stringify({ matrix, degraded, warnings })
  } catch {
    for (let i = 0; i < n; i += 1) {
      for (let j = 0; j < n; j += 1) {
        matrix[i][j] = i === j ? 0 : haversineKm(pts[i], pts[j])
      }
    }
    warnings.push('路网距离获取失败,本次按直线估算排序')
    return JSON.stringify({ matrix, degraded: true, warnings })
  }
}

/** phase=legs:按给定顺序逐段取轨迹/距离/耗时。 */
async function execLegs(provider, key, params) {
  const p = PROVIDERS[provider]
  const tools = await resolveTools(provider, key, ['route'])
  const call = (tool, args) => callTool({ endpoint: p.endpoint(key), transport: p.transport }, tool, args)
  const pts = params.points
  const legs = []

  for (let i = 0; i < pts.length - 1; i += 1) {
    const a = pts[i]
    const b = pts[i + 1]
    const mode = params.mode === 'transit' && provider !== 'amap' ? 'transit' : params.mode
    try {
      if (provider === 'amap') {
        const tool =
          mode === 'transit'
            ? tools.route.transit
            : mode === 'walking'
              ? tools.route.walking
              : tools.route.driving
        const args = { origin: svcCoord(a, p), destination: svcCoord(b, p) }
        if (mode === 'transit') {
          // 高德公交规划强制要求城市(实测),点数据里没有城市信息,此路不通
          throw new Error('amap transit 需要城市参数')
        }
        const { data } = await call(tool, args)
        const path = data?.paths?.[0] || {}
        const km = Number(path.distance) / 1000
        const duration = Number(path.duration) / 60
        // 实测高德 MCP 不返回轨迹几何 → polyline 留空,前端画线回退自托管底图源
        legs.push({
          km: Number.isFinite(km) && km > 0 ? km : haversineKm(a, b),
          duration_min: Number.isFinite(duration) && duration > 0 ? duration : 0,
          polyline: extractPolyline(path).map((s) => toWgs(s, p)).filter(Boolean),
          warnings: Number.isFinite(km) && km > 0 ? [] : [`${a.name}→${b.name} 的路网距离获取失败,已按直线估算`],
        })
      } else {
        const tool = tools.route[mode]
        const args =
          p.coord === 'latlng'
            ? { from: svcCoord(a, p), to: svcCoord(b, p) }
            : { origin: svcCoord(a, p), destination: svcCoord(b, p) }
        const { data } = await call(tool, args)
        const route = data?.result?.routes?.[0] || data?.result?.route || data?.routes?.[0] || {}
        const rawDist = Number(route.distance ?? route.distance_km)
        const km = Number.isFinite(rawDist) && rawDist > 0 ? (rawDist > 1000 ? rawDist / 1000 : rawDist) : haversineKm(a, b)
        legs.push({
          km,
          duration_min: Number(route.duration) || 0,
          polyline: extractPolyline(route).map((s) => toWgs(s, p)).filter(Boolean),
          warnings: [],
        })
      }
    } catch {
      // 单段失败:直线 + 警告,不拖垮整体(与后端降级范式一致)
      legs.push({
        km: haversineKm(a, b),
        duration_min: 0,
        polyline: [],
        warnings: [`${a.name}→${b.name} 的路线获取失败,已按直线估算`],
      })
    }
  }
  return JSON.stringify({ legs })
}

/**
 * 执行一次后端下发的委托任务,返回回填给 /agent/tool_result 的 JSON 字符串。
 * 未绑定/无效绑定时抛错 —— 调用方(useAgent)会把它作为错误结果回填,
 * 模型会据此告知用户。
 */
export async function executeTask(task) {
  const binding = getBinding()
  if (!binding) throw new Error('尚未绑定地图服务')
  const p = PROVIDERS[binding.provider]
  if (!p || !p.web) throw new Error(`${PROVIDERS[binding.provider]?.label || binding.provider} 暂不支持当前平台`)
  if (!binding.key) throw new Error('地图服务 Key 未填写')

  switch (task.phase) {
    case 'search':
      return execSearch(binding.provider, binding.key, task.params)
    case 'matrix':
      return execMatrix(binding.provider, binding.key, task.params)
    case 'legs':
      return execLegs(binding.provider, binding.key, task.params)
    default:
      throw new Error(`未知的执行阶段: ${task.phase}`)
  }
}

/** 绑定校验:tools/list 能拿到列表即认为 Key 可用;返回厂商侧工具数。 */
export async function verifyBinding(provider, key) {
  const p = PROVIDERS[provider]
  if (!p) throw new Error('未知的地图服务')
  if (!p.web) throw new Error('该服务暂不支持网页端直连,请在 App 内绑定')
  const names = await listToolNames({ endpoint: p.endpoint(key), transport: p.transport })
  return names.length
}
