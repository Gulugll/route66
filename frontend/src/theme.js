// theme.js —— 设计变量的「JS 侧镜像」
//
// 为什么 CSS 变量不够用:
// 路线折线颜色通过 new AMap.Polyline({ strokeColor: '#0066CC' }) 设置,
// 高德是命令式 API,只认字符串,读不到 CSS 变量。
// 因此颜色需要一份 JS 可直接引用的定义,即本文件。
//
// ⚠️ 本文件的值与 styles/tokens.css 里的同名变量必须一致,这是一处人工同步点。
// 纯前端无法在构建期消除(需解析 CSS 的构建脚本,当前规模不值得),
// 故加两道保险:
//   1. 每个键都注释了对应的 token 名,修改时对照
//   2. MODE_COLORS 在开发环境实际计算一次 CSS 变量做校验(见文件末尾),
//      不一致即在控制台报错,同步遗漏在开发期暴露

export const COLORS = {
  // —— 中性 ——
  bgPage: '#F5F5F7',      // --bg-page
  bgSurface: '#FFFFFF',   // --bg-surface
  bgSubtle: '#FAFAFC',    // --bg-subtle
  bgSunken: '#F0F0F2',    // --bg-sunken
  border: '#E0E0E0',      // --border
  borderSoft: '#EFEFF1',  // --border-soft

  // —— 文字 ——
  ink: '#1D1D1F',             // --ink
  textSecondary: '#7A7A7A',   // --text-secondary
  textTertiary: '#A1A1A6',    // --text-tertiary

  // —— 语义（出行方式）——
  driving: '#0066CC',   // --driving
  walking: '#34A853',   // --walking
  transit: '#E08600',   // --transit
}

// 出行方式 → 颜色。地图折线、图例色条、模式按钮都用它。
export const MODE_COLORS = {
  driving: COLORS.driving,
  walking: COLORS.walking,
  transit: COLORS.transit,
}

// 出行方式 → 中文名。收口成一张表,新增出行方式只改这里。
export const MODE_LABELS = {
  driving: '驾车',
  walking: '步行',
  transit: '公交',
}

// 出行方式 → 图标名（对应 components/ui.jsx 里的 Icon）
export const MODE_ICONS = {
  driving: 'car',
  walking: 'walk',
  transit: 'bus',
}

export const MODES = ['driving', 'walking', 'transit']

// 页面上还没配高德 key 时的默认视野:北京(天安门附近)。
// 注意高德用 [经度, 纬度],与 [lat, lng] 顺序相反。
export const DEFAULT_CENTER = [116.397, 39.909] // [lng, lat]
export const DEFAULT_ZOOM = 11

// 后端对点数的限制(与 internal/api/router.go 里的常量对应)。
// 前端拿它做即时校验,省掉一次必然失败的请求。
// 两档不同:驾车走批量接口上限 50;步行/公交/混合出行是逐对请求,上限 10。
// 限制按"本次实际走的请求路径"计算,规则以后端为准。
export const MAX_POINTS_BATCH = 50
export const MAX_POINTS_PAIRWISE = 10

// —— 开发期校验：确认 JS 镜像和 CSS 变量一致 ——
// import.meta.env.DEV 是 Vite 提供的编译期常量：
// 开发时为 true，`npm run build` 时整段会被静态剔除，不进生产产物。
if (import.meta.env.DEV && typeof window !== 'undefined') {
  const pairs = [
    ['--driving', COLORS.driving],
    ['--walking', COLORS.walking],
    ['--transit', COLORS.transit],
    ['--ink', COLORS.ink],
  ]
  const css = getComputedStyle(document.documentElement)
  for (const [tokenName, jsValue] of pairs) {
    const cssValue = css.getPropertyValue(tokenName).trim()
    if (cssValue && cssValue.toLowerCase() !== jsValue.toLowerCase()) {
      console.warn(
        `[theme] ${tokenName} 与 theme.js 不一致：CSS=${cssValue} JS=${jsValue}。` +
          '请同步 styles/tokens.css 和 theme.js —— 地图折线的颜色来自 theme.js，' +
          '界面样式来自 tokens.css，不一致会出现"图例和线不同色"。'
      )
    }
  }
}
