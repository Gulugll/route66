// useToasts.js —— 页面上方的提示条
//
// 替代手写版的 alert()。
//
// 这个 hook 演示了"自定义 Hook"的本质：
// **它就是一个用到别的 Hook 的普通函数。** 没有魔法。
// 主要价值是"把一组相关的状态+操作打包"，让组件里干净。
//
// 另一个知识点：**清理定时器**。
// 每条提示 6 秒后自动消失，靠 setTimeout。但组件卸载时如果不管它，
// 定时器还会在 6 秒后回调 —— 那时组件已经没了，React 会在控制台警告
// "更新了已卸载的组件"。所以要把定时器 id 存起来、在卸载时清掉。
// 这里用了一个更简单的写法：让 setToasts 自己去 filter，
// 即使组件卸载了也只是对一个已经不存在的东西 setState（React 19 会安静忽略）。

import { useCallback, useRef, useState } from 'react'

const AUTO_DISMISS_MS = 6000

export function useToasts() {
  const [toasts, setToasts] = useState([])
  const seqRef = useRef(0)

  const dismiss = useCallback((id) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const push = useCallback(
    (text, kind = 'error') => {
      seqRef.current += 1
      const id = seqRef.current
      setToasts((prev) => [...prev, { id, text, kind }])

      // 自动消失。存一下定时器，是为了"用户手动关掉之后，
      // 定时器到点时再做一次 filter 也无害" —— filter 是幂等的。
      setTimeout(() => {
        setToasts((prev) => prev.filter((t) => t.id !== id))
      }, AUTO_DISMISS_MS)
    },
    []
  )

  const clear = useCallback(() => setToasts([]), [])

  return { toasts, push, dismiss, clear }
}
