// useAgent.js —— RouteBot 对话链路的状态管理
//
// 消息流的数据形状(threads):
//   {kind:'user', text}   用户气泡
//   {kind:'steps', steps} 工具调用过程(可折叠的步骤链)
//   {kind:'bot', text}    RouteBot 的回答
//   {kind:'error', text}  本轮失败原因
//
// 后端的 NDJSON 事件:step(工具轮) / delta(正文增量) / think(推理模型思考)
// / done(最终答案) / error。delta/think 让"漫长的推理等待"变成看得见的流。
//
// 发给后端的历史(llmHistory)与展示用的 threads 分开维护:
// 后端只要纯文本 user/assistant,中间的工具调用过程不可重放、也不该重放。
//
// 联动:onPlan(ordered, result, mode) 在 RouteBot 用 plan_route 算出方案时
// 被调用 —— 上层把它接进规划带/地图,方案就不只是"一段话"而是"可执行的状态"。

import { useCallback, useRef, useState } from 'react'
import { streamAgent } from '../api.js'

export function useAgent({ onPlan } = {}) {
  const [threads, setThreads] = useState([])
  const [busy, setBusy] = useState(false)
  const [streamText, setStreamText] = useState('')
  const [thinkText, setThinkText] = useState('')
  // unavailable: 后端返回"未配置 LLM_API_KEY"时置真 —— 面板切换为禁用态,
  // 不再让用户对着一个注定失败的输入框打字。
  const [unavailable, setUnavailable] = useState(false)
  // 发给后端的纯文本历史,ref 即可:它只被 send 顺序读写,不驱动渲染
  const llmHistory = useRef([])
  const busyRef = useRef(false)

  const send = useCallback(
    async (text) => {
      const q = text.trim()
      if (!q || busyRef.current) return
      busyRef.current = true
      setBusy(true)
      setStreamText('')
      setThinkText('')

      setThreads((prev) => [...prev, { kind: 'user', text: q }, { kind: 'steps', steps: [] }])
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
              const calls = st.calls || []
              const results = st.results || []
              patchLastSteps((prev) => [
                ...prev,
                {
                  iteration: st.iteration,
                  calls: calls.map((c) => ({
                    name: c.function?.name || '?',
                    args: c.function?.arguments || '',
                  })),
                },
              ])
              // ── 联动:plan_route 的结果同步进工作台 ──
              calls.forEach((c, i) => {
                if (c.function?.name !== 'plan_route' || !onPlan) return
                try {
                  const result = JSON.parse(results[i] || 'null')
                  if (!result?.order_idx) return
                  const args = JSON.parse(c.function.arguments || '{}')
                  const pts = args.points || []
                  const ordered = result.order_idx.map((idx) => pts[idx]).filter(Boolean)
                  if (ordered.length >= 2) onPlan(ordered, result, args.mode || 'driving')
                } catch {
                  // 结果不是合法 JSON(如工具报错文本)就放弃联动,不影响对话
                }
              })
            } else if (evt.type === 'delta') {
              setStreamText((prev) => prev + evt.text)
            } else if (evt.type === 'think') {
              setThinkText(evt.text)
            } else if (evt.type === 'done') {
              answer = evt.answer || ''
            } else if (evt.type === 'error') {
              throw new Error(evt.error || 'agent 执行失败')
            }
          },
        })

        if (answer) {
          llmHistory.current.push({ role: 'assistant', content: answer })
          setThreads((prev) => [...prev, { kind: 'bot', text: answer }])
        } else {
          setThreads((prev) => [...prev, { kind: 'error', text: '本轮没有得到回答，请换个问法重试' }])
        }
      } catch (err) {
        const msg = err.name === 'AbortError' ? '已停止' : err.message
        if (msg.includes('未配置 LLM')) setUnavailable(true)
        // 失败后把悬空的 user 消息从后端历史里去掉,避免下一轮历史错位
        llmHistory.current = llmHistory.current.filter(
          (m, i, arr) => !(i === arr.length - 1 && m.role === 'user')
        )
        setThreads((prev) => [...prev, { kind: 'error', text: msg }])
      } finally {
        busyRef.current = false
        setBusy(false)
        setStreamText('')
        setThinkText('')
      }
    },
    [onPlan]
  )

  const clear = useCallback(() => {
    llmHistory.current = []
    setThreads([])
  }, [])

  return { threads, busy, unavailable, streamText, thinkText, send, clear }
}
