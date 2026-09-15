// theme.js —— 设计变量的「JS 侧镜像」
//
// 为什么 CSS 变量不够用：
// 路线折线的颜色是这样设的 —— new AMap.Polyline({ strokeColor: '#0066CC' })。
// 这是**命令式** API，它只认字符串，读不到 CSS 变量。
// 所以颜色必须有一份 JS 能直接引用的定义 —— 就是这个文件。
//
// ⚠️ 代价与防错：
// 这里的值和 styles/tokens.css 里的同名变量必须一致，这是一处"人工同步点"。
// 我没法在纯前端消除它（真要在构建期消掉，得写个脚本把 CSS 解析成 JS，
// 对这个规模的项目属于过度工程）。所以我加了两道保险：
//
//   1. 下面每个键都注释了它对应 token 名字，改的时候对着看
//   2. MODE_COLORS 在开发环境会去实际计算一次 CSS 变量做校验（见文件末尾），
//      不一致就在控制台报出来 —— 让"忘了同步"这件事在开发时立刻暴露，
//      而不是等用户发现地图上的线颜色不对。
//
// 教学点：这叫"运行时断言"。写代码时防不住的问题，可以让程序自己喊出来。

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
// 手写版里这个映射函数叫 modeColor()，现在它只是这张表的一次查表。
export const MODE_COLORS = {
  driving: COLORS.driving,
  walking: COLORS.walking,
  transit: COLORS.transit,
}

// 出行方式 → 中文名。
// 手写版里这个映射散在两处（按钮上没用文字、图例里写死了"驾车/步行/公交"），
// 现在收口成一张表 —— 加一种出行方式只要改这一个对象。
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

// 页面上还没配高德 key 时的默认视野：北京（天安门附近）。
// 注意高德用 [经度, 纬度]，和 [lat, lng] 的顺序**相反** —— 这个坑踩过。
export const DEFAULT_CENTER = [116.397, 39.909] // [lng, lat]
export const DEFAULT_ZOOM = 11

// 后端对点数的限制（和 internal/api/router.go 里的常量对应）。
// 前端拿它做即时校验，省掉一次必然失败的请求。
// 注意这两档不一样：驾车走批量接口能到 50，步行/公交/混合出行是逐对请求，只有 10。
// 限制按"最坏路径"算 —— 这是后端的规矩，前端照抄一份做提示。
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
