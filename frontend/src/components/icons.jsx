// icons.jsx —— 图标
//
// 每个图标都是一个普通函数，接收 props，返回一段 JSX。
// 这就是 React 里"组件"的全部定义 —— 没有类、没有装饰器、没有模板语法。
//
// 两个刻意的设计：
//
// 1) 描边用 currentColor 而不是写死的颜色。
//    SVG 里的 currentColor 取的是 CSS 的 color 值。所以我们写一次图标，
//    它在墨色按钮里就是白的、在灰色提示里就是灰的 —— 颜色由**使用位置**决定，
//    而不是由图标自己决定。手写版是直接把色值写进 SVG 字符串，等于把
//    "图标的颜色"和"在哪用"耦合死了。
//
// 2) 统一走一个 <Icon name="..."> 分发器。
//    调用方写 <Icon name="car" />，不用记住具体组件叫什么。
//    代价是所有图标都会被打进产物（没法 tree-shaking）——
//    十几个图标的量级完全无所谓，真到几百个时该换成直接 import 具体组件。

const STROKE = {
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
}

/** base 提供所有图标共用的外壳：尺寸、viewBox、颜色继承。 */
function Svg({ size = 16, children, ...rest }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true" /* 图标是装饰，读屏应该跳过它；文字已经说明了含义 */
      {...rest}
    >
      {children}
    </svg>
  )
}

// —— 品牌 ——
const Route = (p) => (
  <Svg {...p}>
    <path d="M6 18C6 12.5 18 11.5 18 6" {...STROKE} />
    <circle cx="6" cy="19.4" r="2.4" fill="currentColor" />
    <circle cx="18" cy="4.6" r="2.4" fill="currentColor" />
  </Svg>
)

const Sliders = (p) => (
  <Svg {...p}>
    <path d="M3 7h18M3 12h18M3 17h18" {...STROKE} />
    <circle cx="8.5" cy="7" r="2.6" fill="var(--bg-surface)" {...STROKE} />
    <circle cx="15.5" cy="12" r="2.6" fill="var(--bg-surface)" {...STROKE} />
    <circle cx="7.5" cy="17" r="2.6" fill="var(--bg-surface)" {...STROKE} />
  </Svg>
)

// —— 通用 ——
const Search = (p) => (
  <Svg {...p}>
    <circle cx="11" cy="11" r="7" {...STROKE} />
    <path d="M16.2 16.2L20.6 20.6" {...STROKE} />
  </Svg>
)

const Pin = (p) => (
  <Svg {...p}>
    <path d="M12 21.5s7-6.1 7-11a7 7 0 10-14 0c0 4.9 7 11 7 11z" {...STROKE} />
    <circle cx="12" cy="10.2" r="2.6" {...STROKE} />
  </Svg>
)

const Close = (p) => (
  <Svg {...p}>
    <path d="M6 6l12 12M18 6L6 18" {...STROKE} strokeWidth={2} />
  </Svg>
)

const ChevronRight = (p) => (
  <Svg {...p}>
    <path d="M9 5l7 7-7 7" {...STROKE} strokeWidth={2} />
  </Svg>
)

const ArrowRight = (p) => (
  <Svg {...p}>
    <path d="M4 12h15M13 6l6 6-6 6" {...STROKE} strokeWidth={1.9} />
  </Svg>
)

const Plus = (p) => (
  <Svg {...p}>
    <path d="M12 5v14M5 12h14" {...STROKE} strokeWidth={2} />
  </Svg>
)

const Minus = (p) => (
  <Svg {...p}>
    <path d="M5 12h14" {...STROKE} strokeWidth={2} />
  </Svg>
)

const Check = (p) => (
  <Svg {...p}>
    <path d="M5 12.5l4.5 4.5L19 7" {...STROKE} strokeWidth={2.6} />
  </Svg>
)

// 实心圆 + 白勾：需要两个颜色，所以勾用固定白色（它永远在深色底上）
const CheckCircle = (p) => (
  <Svg {...p}>
    <circle cx="12" cy="12" r="9" fill="currentColor" />
    <path d="M7.8 12.3l2.8 2.8 5.6-6" fill="none" stroke="#FFFFFF" strokeWidth={2.2} strokeLinecap="round" strokeLinejoin="round" />
  </Svg>
)

const Clock = (p) => (
  <Svg {...p}>
    <circle cx="12" cy="12" r="8.6" {...STROKE} strokeWidth={2} />
    <path d="M12 7.4V12l3.2 2" {...STROKE} strokeWidth={2} />
  </Svg>
)

const Circle = (p) => (
  <Svg {...p}>
    <circle cx="12" cy="12" r="8.6" {...STROKE} strokeWidth={2} />
  </Svg>
)

const Spinner = (p) => (
  <Svg {...p}>
    <circle cx="12" cy="12" r="8.5" fill="none" stroke="currentColor" strokeWidth={2.6} opacity={0.3} />
    <path d="M12 3.5a8.5 8.5 0 018.5 8.5" fill="none" stroke="currentColor" strokeWidth={2.6} strokeLinecap="round" />
  </Svg>
)

const AlertTriangle = (p) => (
  <Svg {...p}>
    <path d="M12 3.6l9 15.8H3l9-15.8z" {...STROKE} />
    <path d="M12 9.6v4.2" {...STROKE} strokeWidth={1.9} />
    <circle cx="12" cy="16.6" r="1.1" fill="currentColor" />
  </Svg>
)

const AlertCircle = (p) => (
  <Svg {...p}>
    <circle cx="12" cy="12" r="9" {...STROKE} strokeWidth={1.9} />
    <path d="M12 7.6v5.2" {...STROKE} strokeWidth={1.9} />
    <circle cx="12" cy="16.4" r="1.05" fill="currentColor" />
  </Svg>
)

// —— 出行方式 ——
const Car = (p) => (
  <Svg {...p}>
    <path d="M5 14.5l1.9-4.6A1.6 1.6 0 018.35 9h7.3a1.6 1.6 0 011.45.9L19 14.5" {...STROKE} strokeWidth={1.7} />
    <path d="M4 14.5h16v3.2a1 1 0 01-1 1h-1.6a1 1 0 01-1-1v-1H7.6v1a1 1 0 01-1 1H5a1 1 0 01-1-1v-3.2z" {...STROKE} strokeWidth={1.7} />
  </Svg>
)

const Walk = (p) => (
  <Svg {...p}>
    <circle cx="13" cy="4.6" r="2.2" {...STROKE} strokeWidth={1.7} />
    <path d="M13 7.6l-1.8 4.6 2.6 2 1 5.4" {...STROKE} strokeWidth={1.7} />
    <path d="M11.2 12.2l-3 1.8M14 14.2l3 1.2" {...STROKE} strokeWidth={1.7} />
  </Svg>
)

const Bus = (p) => (
  <Svg {...p}>
    <rect x="4.5" y="3.5" width="15" height="13" rx="2.6" {...STROKE} strokeWidth={1.7} />
    <path d="M7.5 8h9M4.5 12.5h15" fill="none" stroke="currentColor" strokeWidth={1.5} />
    <circle cx="7.8" cy="19.2" r="1.8" {...STROKE} strokeWidth={1.7} />
    <circle cx="16.2" cy="19.2" r="1.8" {...STROKE} strokeWidth={1.7} />
  </Svg>
)

// 一张表把 name 映射到组件。
// 加图标 = 写一个函数 + 在这张表里加一行，调用方不用动。
const REGISTRY = {
  route: Route,
  sliders: Sliders,
  search: Search,
  pin: Pin,
  close: Close,
  chevronRight: ChevronRight,
  arrowRight: ArrowRight,
  plus: Plus,
  minus: Minus,
  check: Check,
  checkCircle: CheckCircle,
  clock: Clock,
  circle: Circle,
  spinner: Spinner,
  alertTriangle: AlertTriangle,
  alertCircle: AlertCircle,
  car: Car,
  walk: Walk,
  bus: Bus,
}

/**
 * 按名字渲染图标。
 *
 * {...rest} 是 JS 的**对象展开**：调用方传进来的其它 props（className、style、
 * onClick、aria-label…）会原样落到 <svg> 上。所以这个组件不需要知道
 * 调用方想传什么 —— 这就是为什么 React 组件一般不去逐条列 props。
 */
export function Icon({ name, size = 16, ...rest }) {
  const Cmp = REGISTRY[name]
  if (!Cmp) {
    // 名字写错时不要静默 —— 开发时立刻看得见
    console.warn(`[Icon] 未知图标: ${name}`)
    return null
  }
  return <Cmp size={size} {...rest} />
}
