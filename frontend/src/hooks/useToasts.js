// useToasts.js —— 页面上方的提示条
//
// 自定义 Hook:用到其他 Hook 的普通函数,把一组相关状态与操作打包。
//
// 自动消失用 setTimeout 实现。组件卸载后定时器仍会触发,
// 但这里只是对已不存在的条目做幂等的 filter(React 19 对卸载后的
// setState 静默忽略),无需额外清理定时器。

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
