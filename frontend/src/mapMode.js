// mapMode.js —— 当前地图模式的模块级单例。
//
// 模式由后端决定(DISTANCE_SOURCE → /config/public 的 mode 字段):
//   amap = 高德模式(高德底图,GCJ-02);oss = 开源模式(OSM 底图,WGS-84)。
// App 启动时写入一次,进程生命周期内不变 —— 所以用模块变量而不是 React state:
// 消费者(useLeaflet / MapView / PostMap)分属不同子树,为不变的值做 props 钻透不值当。
// 时序:地图容器在登录墙后/详情页才挂载,config 早已拉完,初始化读到的一定是最终值。
let mode = 'oss'

export function setMapMode(m) {
  if (m === 'amap' || m === 'oss') mode = m
}

export function getMapMode() {
  return mode
}
