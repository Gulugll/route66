// main.jsx —— 应用的入口（整个 React 应用从这里开始）
//
// 就三件事：引入样式、找到挂载点、把 <App/> 渲染进去。
// 之后所有 DOM 都由 React 生成 —— index.html 里那个空 div 是最后一处手写 DOM。

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import App from './App.jsx'

// 样式分两个文件：
//   tokens.css —— 设计变量（颜色/圆角/间距/字体）。只声明变量，不写选择器。
//   app.css    —— 布局与组件样式，全部引用上面的变量。
// 分开的意义：改配色只动一个文件，且改完整个界面一起变。
import './styles/tokens.css'
import './styles/app.css'

// createRoot：React 18 之后的新入口，替代了旧的 ReactDOM.render。
// 它的作用是创建一棵"React 管理的树"，#root 里的内容从此归 React 管。
const root = createRoot(document.getElementById('root'))

root.render(
  // StrictMode 只影响开发环境,生产构建无操作。
  //
  // 它会把每个 effect 故意"挂载 → 卸载 → 再挂载"一遍,
  // 用于验证清理函数是否正确:未清理的监听/定时器/地图实例会立刻现形,
  // 而不是上线后才慢慢泄漏。
  //
  // 本项目中 useAmap(地图销毁)、MapView(高德事件监听)、
  // App(登录墙三分支)都经过它的验证。
  <StrictMode>
    <App />
  </StrictMode>
)
