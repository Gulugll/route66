import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// vite.config.js —— 这个文件决定了"开发怎么跑"和"打包放在哪"。
//
// 两个关键点，都跟"前后端分离但同源"这个前提有关：
//
// 1) base: '/'
//    React 打包出来的 HTML 里会引用 /assets/xxx.js，前缀就是 base。
//    产物由 Go 托管在**站点根路径**下，所以这里是 '/'。
//
//    ⚠️ base 和 outDir 是一对，必须指向同一个位置：
//      base   决定"HTML 里怎么写资源路径"
//      outDir 决定"文件实际放在哪"
//    以前产物挂在 /app/ 时，这两个写的是 '/app/' 和 '../web/app'；
//    现在改成根路径，两个一起改。只改一个就会 404。
//
// 2) server.proxy
//    开发时前端跑在 Vite 自己的 5173 端口，后端还是 7800，两个不同源 →
//    浏览器会因为跨域(CORS)拦掉 fetch。proxy 的意思是：
//    "凡是请求 /plan、/search、/route 的，Vite 开发服务器替我转发给 7800"。
//    对浏览器来说这些请求始终是同源(5173)的，CORS 问题根本不会出现。
//    注意这**只影响开发**，生产环境页面由 Go 直接托管，天然同源。
const BACKEND = 'http://localhost:7800'

export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    // 产物直接丢进 web/ —— gin 的 NoRoute 托管的就是整个 web/ 目录，
    // 所以 / 就是 React 版，**Go 代码一行都不用改**。
    //
    // emptyOutDir 会先清空 web/ 再写入，好处是"半新半旧"的文件不会残留
    // （原来 web/ 根下的手写版 index.html + app.js + vendor/ 就是被它清掉的，
    //   不会出现"改了个名字结果旧文件还在、浏览器加载了旧的"这种情况）。
    outDir: '../web',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/plan': BACKEND,
      '/search': BACKEND,
      '/route': BACKEND,
      '/healthz': BACKEND,
    },
  },
})
