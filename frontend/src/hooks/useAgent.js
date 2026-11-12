// useAgent.js —— RouteBot 对话链路的状态管理
//
// 消息流的数据形状(threads):
//   {kind:'user', text}              用户气泡
//   {kind:'steps', steps}            工具调用过程(可折叠的步骤链)
//   {kind:'bot', text}               RouteBot 的回答
//   {kind:'error', text}             本轮失败原因
//   {kind:'confirm', sessionId, prompt, options}  HITL 确认卡片(等用户拍板)
//
// 后端的 NDJSON 事件:step(工具轮) / delta(正文增量) / think(推理模型思考)
// / confirm(等待用户确认) / done(最终答案) / error。
//
// 发给后端的历史(llmHistory)与展示用的 threads 分开维护:
// 后端只要纯文本 user/assistant,中间的工具调用过程不可重放、也不该重放。
// HITL 挂起轮不属于 llmHistory —— 它的状态在服务端会话里,答复经 /agent/resume 送达。
//
// 联动:onPlan(ordered, result, mode) 在 RouteBot 用 plan_route 算出方案时
// 被调用 —— 上层把它接进规划带/地图,方案就不只是"一段话"而是"可执行的状态"。

import { useCallback, useRef, useState } from 'react'
import { agentToolResult, resumeAgent, streamAgent } from '../api.js'
import { executeTask, PROVIDERS } from '../mcp/providers.js'
import { getBinding } from '../mcp/settings.js'

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

  // runStream 执行一次对话(首跑或续跑),消费 NDJSON 事件并渲染。
  // initiator: () => Promise —— streamAgent(...) 或 resumeAgent(...)。
  // 返回 'done' | 'confirmed' | 'error'。
  const runStream = useCallback(
    async (initiator) => {
      let answer = ''
      let confirmed = false
      // 委托执行:流以 tool_exec 事件收尾,任务信息带出给上层循环
      let execInfo = null
      const patchLastSteps = (fn) => {
        setThreads((prev) => {
          const next = [...prev]
          const last = next[next.length - 1]
          if (last?.kind === 'steps') next[next.length - 1] = { ...last, steps: fn(last.steps) }
          return next
        })
      }

      await initiator({
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
            // MCP 模式的结果是 McpPlanResult(order_idx + legs),同样带 order_idx
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
          } else if (evt.type === 'confirm') {
            // HITL:本轮以确认卡片收尾。挂起不是失败,不再期待 done。
            confirmed = true
            setThreads((prev) => [
              ...prev,
              {
                kind: 'confirm',
                sessionId: evt.session_id,
                prompt: evt.prompt || '请确认',
                options: evt.options || [],
              },
            ])
          } else if (evt.type === 'tool_exec') {
            // 委托执行:结果由本设备直连地图 MCP 产出后回填(见 send/respond 的续跑循环)
            execInfo = {
              sessionId: evt.session_id,
              phase: evt.phase,
              params: evt.params || '{}',
              hint: evt.hint || '正在通过绑定的地图服务执行…',
            }
            const label = PROVIDERS[getBinding()?.provider || '']?.label || ''
            setThreads((prev) => [
              ...prev,
              { kind: 'exec', hint: execInfo.hint, provider: label },
            ])
          } else if (evt.type === 'done') {
            answer = evt.answer || ''
          } else if (evt.type === 'error') {
            throw new Error(evt.error || 'agent 执行失败')
          }
        },
      })

      if (execInfo) return { status: 'exec', exec: execInfo }
      if (confirmed) return { status: 'confirmed' }
      if (answer) {
        llmHistory.current.push({ role: 'assistant', content: answer })
        setThreads((prev) => [...prev, { kind: 'bot', text: answer }])
        return { status: 'done' }
      }
      setThreads((prev) => [...prev, { kind: 'error', text: '本轮没有得到回答，请换个问法重试' }])
      return { status: 'error' }
    },
    [onPlan],
  )

  // runWithExecLoop 流式消费 + 委托执行的续跑循环。
  // plan_route 在 MCP 模式是两阶段任务(matrix → legs),每轮回填可能再触发
  // 下一轮 tool_exec —— 循环到不再挂起为止。
  const runWithExecLoop = useCallback(
    async (startInitiator) => {
      let initiator = startInitiator
      for (;;) {
        const r = await runStream(initiator)
        if (r.status !== 'exec') return r.status
        let payload
        try {
          payload = await executeTask({
            phase: r.exec.phase,
            params: JSON.parse(r.exec.params || '{}'),
          })
        } catch (err) {
          // 执行失败也回填:错误文本进模型历史,由它向用户解释,循环不中断
          payload = { error: err.message }
        }
        initiator = (opts) => agentToolResult(r.exec.sessionId, payload, opts)
      }
    },
    [runStream],
  )

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

      try {
        await runWithExecLoop((opts) => streamAgent(llmHistory.current, { ...opts, mapProvider: getBinding()?.provider || '' }))
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
    [runWithExecLoop]
  )

  // respond 用户在确认卡片上答复后续跑。答复作为 user 气泡展示 ——
  // "谁在说话"要看得见,卡片按钮的点击和手打输入在界面上等价。
  const respond = useCallback(
    async (sessionId, answer) => {
      if (!sessionId || !answer || busyRef.current) return
      busyRef.current = true
      setBusy(true)
      setStreamText('')
      setThinkText('')

      setThreads((prev) => [
        // 把已答复的卡片从"待确认"变成历史:过滤掉当前这张,避免残留可重复点击的按钮
        ...prev.filter(
          (t) => !(t.kind === 'confirm' && t.sessionId === sessionId)
        ),
        { kind: 'user', text: answer },
        { kind: 'steps', steps: [] },
      ])

      try {
        await runWithExecLoop((opts) => resumeAgent(sessionId, answer, opts))
      } catch (err) {
        const msg = err.name === 'AbortError' ? '已停止' : err.message
        setThreads((prev) => [...prev, { kind: 'error', text: msg }])
      } finally {
        busyRef.current = false
        setBusy(false)
        setStreamText('')
        setThinkText('')
      }
    },
    [runWithExecLoop]
  )

  const clear = useCallback(() => {
    llmHistory.current = []
    setThreads([])
  }, [])

  return { threads, busy, unavailable, streamText, thinkText, send, respond, clear }
}
