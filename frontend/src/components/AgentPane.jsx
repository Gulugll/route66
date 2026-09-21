// AgentPane.jsx —— 右侧 RouteBot 面板
//
// 消息流 / 工具步骤链 / 建议提问 / 输入框。对话状态在 useAgent,
// 本组件只负责渲染与收发;可折叠由父级(App)控制,因为收起按钮
// 出现在面板上、展开入口(浮动按钮)出现在地图上,两者必须共享状态。

import { useEffect, useRef, useState } from 'react'
import { useAgent } from '../hooks/useAgent.js'

const SUGGESTIONS = [
  '帮我规划一条从天安门出发，途经故宫再到天坛的驾车路线',
  '从北京站到颐和园，公交怎么走？大概多久？',
  '天坛附近有什么地铁站？',
]

function ToolSteps({ steps }) {
  const [open, setOpen] = useState(true)
  const count = steps.reduce((n, s) => n + (s.calls?.length || 0), 0)
  return (
    <div className="tool-steps">
      <div
        className="tool-steps-head"
        onClick={() => setOpen((v) => !v)}
        role="button"
        aria-expanded={open}
      >
        <span>工具调用{open ? '' : ' · 已折叠'}</span>
        <span className="n">{count} 次</span>
      </div>
      {open &&
        steps.map((s) =>
          (s.calls || []).map((c, i) => (
            <div className="tool-step" key={`${s.iteration}-${i}`}>
              <span className="name">{c.name}</span>
              <span className="arg">{truncate(c.args, 46)}</span>
              <span className="ok" style={{ fontSize: 11, color: 'var(--accent)' }}>✓</span>
            </div>
          ))
        )}
    </div>
  )
}

function truncate(s, n) {
  return s.length > n ? s.slice(0, n) + '…' : s
}

export function AgentPane({ onCollapse, onPlan }) {
  const { threads, busy, unavailable, streamText, thinkText, send } = useAgent({ onPlan })
  const [input, setInput] = useState('')
  const inputRef = useRef(null)
  // 消息区自动滚底:新气泡/流式增量出现时跟随,用户手动上滚则不打扰
  const bodyRef = useRef(null)
  const followRef = useRef(true)
  const onScroll = () => {
    const el = bodyRef.current
    if (!el) return
    followRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
  }
  useEffect(() => {
    const el = bodyRef.current
    if (el && followRef.current) el.scrollTop = el.scrollHeight
  })

  const submit = () => {
    if (!input.trim() || busy || unavailable) return
    const q = input
    setInput('')
    // 发送后把焦点还给输入框:连续追问是最常见的路径
    inputRef.current?.focus()
    send(q)
  }

  return (
    <aside className="agent-pane" aria-label="RouteBot 智能助手">
      <div className="agent-head">
        <div className="agent-title">
          <span className={`status-dot${unavailable ? ' off' : ''}`} />
          RouteBot
          <span className="agent-sub">路线智能助手</span>
        </div>
        <button className="agent-collapse" onClick={onCollapse}>收起 ▶</button>
      </div>

      <div className="agent-body" ref={bodyRef} onScroll={onScroll}>
        {threads.length === 0 && !unavailable && (
          <div className="suggest">
            <span className="suggest-label">试试这样问</span>
            {SUGGESTIONS.map((s) => (
              <button
                key={s}
                className="suggest-btn"
                onClick={() => {
                  setInput('')
                  send(s)
                }}
              >
                {s}
              </button>
            ))}
          </div>
        )}

        {threads.map((m, i) => {
          if (m.kind === 'user') {
            return (
              <div key={i} className="msg msg-user">{m.text}</div>
            )
          }
          if (m.kind === 'steps') {
            return <ToolSteps key={i} steps={m.steps} />
          }
          if (m.kind === 'error') {
            return <div key={i} className="msg msg-error">{m.text}</div>
          }
          return <div key={i} className="msg msg-bot">{m.text}</div>
        })}

        {/* 流式区:思考过程(推理模型) + 正文逐字输出。
            done 后 streamText 清空,最终答案以完整气泡落入 threads。 */}
        {busy && thinkText && !streamText && (
          <div className="msg msg-think">思考中 · {truncate(thinkText, 120)}</div>
        )}
        {busy && streamText && (
          <div className="msg msg-bot">
            {streamText}
            <span className="caret" />
          </div>
        )}
        {busy && !streamText && !thinkText && threads[threads.length - 1]?.kind === 'steps' && (
          <div className="msg msg-bot"><span className="caret" /></div>
        )}

        {unavailable && (
          <div className="msg msg-error">
            RouteBot 未启用：管理员需在 .env 或管理台配置 LLM_API_KEY / LLM_BASE_URL / LLM_MODEL 后重启服务。
          </div>
        )}
      </div>

      <div className="agent-input">
        <input
          ref={inputRef}
          type="text"
          placeholder={unavailable ? 'RouteBot 未启用' : '向 RouteBot 描述你的出行计划…'}
          value={input}
          disabled={unavailable}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') submit()
          }}
        />
        <button className="agent-send" onClick={submit} disabled={busy || unavailable}>
          发送
        </button>
      </div>
      <div className="agent-hint">回答基于工具返回的真实数据；规划结果可同步到下方规划带</div>
    </aside>
  )
}
