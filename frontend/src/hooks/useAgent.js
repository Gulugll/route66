// useAgent.js —— RouteBot 对话链路的状态管理
//
// 消息流的数据形状（threads）:
//   {kind:'user', text}   用户气泡
//   {kind:'steps', steps} 一轮工具调用过程（可折叠的步骤链）
//   {kind:'bot', text}    RouteBot 的回答
//   {kind:'error', text}  本轮失败原因
//
// 发给后端的历史(llmHistory)与展示用的 threads 分开维护:
// 后端只要纯文本 user/assistant,中间的工具调用过程不可重放、也不该重放。

import { useCallback, useRef, useState } from 'react'
import { streamAgent } from '../api.js'

export function useAgent() {
  const [threads, setThreads] = useState([])
  const [busy, setBusy] = useState(false)
  // unavailable: 后端返回"未配置 LLM_API_KEY"时置真 —— 面板切换为禁用态,
  // 不再让用户对着一个注定失败的输入框打字。
  const [unavailable, setUnavailable] = useState(false)
  // 发给后端的纯文本历史,ref 即可:它只被 send 顺序读写,不驱动渲染
  const llmHistory = useRef([])
  const busyRef = useRef(false)

  const send = useCallback(async (text) => {
    const q = text.trim()
    if (!q || busyRef.current) return
    busyRef.current = true
    setBusy(true)

    setThreads((prev) => [
      ...prev,
      { kind: 'user', text: q },
      { kind: 'steps', steps: [] },
    ])
    llmHistory.current.push({ role: 'user', content: q })

    const patchLastSteps = (fn) => {
      setThreads((prev) => {
        const next = [...prev]
        const last = next[next.length - 1]
        if (last?.kind === 'steps') next[next.length - 1] = { ...last, steps: fn(last.steps) }
        return next
      })
    }

    try {
      let answer = ''
      await streamAgent(llmHistory.current, {
        onEvent: (evt) => {
          if (evt.type === 'step') {
            const st = evt.step || {}
            const calls = (st.calls || []).map((c) => ({
              name: c.function?.name || '?',
              args: c.function?.arguments || '',
              results: st.results || [],
            }))
            patchLastSteps((prev) => [...prev, { iteration: st.iteration, calls }])
          } else if (evt.type === 'done') {
            answer = evt.answer || ''
          } else if (evt.type === 'error') {
            throw new Error(evt.error || 'agent 执行失败')
          }
        },
      })

      if (answer) {
        llmHistory.current.push({ role: 'assistant', content: answer })
        setThreads((prev) => [...prev.slice(0, -1), { kind: 'bot', text: answer }])
      } else {
        // 没有答案也没有抛错(如超限刹车):把步骤占位改成一条说明
        setThreads((prev) => [...prev.slice(0, -1), { kind: 'error', text: '本轮没有得到回答，请换个问法重试' }])
      }
    } catch (err) {
      const msg = err.name === 'AbortError' ? '已停止' : err.message
      if (msg.includes('未配置 LLM')) setUnavailable(true)
      // 失败的用户消息仍留在历史里,但通知后端历史时要去掉这条悬空的 user
      llmHistory.current = llmHistory.current.filter(
        (m, i, arr) => !(i === arr.length - 1 && m.role === 'user')
      )
      setThreads((prev) => [...prev.slice(0, -1), { kind: 'error', text: msg }])
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }, [])

  const clear = useCallback(() => {
    llmHistory.current = []
    setThreads([])
  }, [])

  return { threads, busy, unavailable, send, clear }
}
