// AgentPane.jsx —— 右侧 RouteBot 面板
//
// 消息流 / 工具步骤链 / 建议提问 / 输入框。对话状态在 useAgent,
// 本组件只负责渲染与收发;可折叠由父级(App)控制,因为收起按钮
// 出现在面板上、展开入口(浮动按钮)出现在地图上,两者必须共享状态。

import { useEffect, useRef, useState } from 'react'
import { useAgent } from '../hooks/useAgent.js'
import { PROVIDERS, verifyBinding } from '../mcp/providers.js'
import { clearBinding, getBinding, saveBinding } from '../mcp/settings.js'

// 地图数据源绑定:key 只存本设备 localStorage,调用由浏览器直连所选厂商,
// 后端不经手密钥(合规立场的实现落点)。百度端点无 CORS 头,Web 端禁选。
function MapSourceSettings() {
  const [open, setOpen] = useState(false)
  const [provider, setProvider] = useState('')
  const [key, setKey] = useState('')
  const [status, setStatus] = useState(null) // {tone:'ok'|'error', text}
  const [checking, setChecking] = useState(false)
  const bound = getBinding()

  const bind = async () => {
    if (!provider || !key.trim()) {
      setStatus({ tone: 'error', text: '请选择服务并填写 Key' })
      return
    }
    setChecking(true)
    setStatus(null)
    try {
      const count = await verifyBinding(provider, key.trim())
      saveBinding(provider, key.trim())
      setStatus({ tone: 'ok', text: `已连接 ${PROVIDERS[provider].label}（${count} 个工具可用）` })
    } catch (err) {
      setStatus({ tone: 'error', text: err.message })
    } finally {
      setChecking(false)
    }
  }

  return (
    <div className="map-source">
      <button className="map-source-toggle" onClick={() => setOpen((v) => !v)}>
        地图数据源：{bound ? `已绑定 ${PROVIDERS[bound.provider]?.label || bound.provider}` : '默认'}
      </button>
      {open && (
        <div className="map-source-panel">
          <select value={provider} onChange={(e) => setProvider(e.target.value)}>
            <option value="">选择要绑定的服务…</option>
            {Object.entries(PROVIDERS).map(([id, p]) => (
              <option key={id} value={id} disabled={!p.web}>
                {p.label}
                {!p.web ? '（仅 App 端可用）' : ''}
              </option>
            ))}
          </select>
          <input
            type="password"
            placeholder={provider ? '粘贴你的 Web服务 Key' : '先选服务再填 Key'}
            value={key}
            onChange={(e) => setKey(e.target.value)}
          />
          <button onClick={bind} disabled={checking}>
            {checking ? '校验中…' : '校验并绑定'}
          </button>
          {bound && (
            <button onClick={() => { clearBinding(); setKey(''); setStatus({ tone: 'ok', text: '已恢复默认数据源' }) }}>
              恢复默认
            </button>
          )}
          <div className="map-source-note">
            Key 属于你本人，保存在本设备；查询请求由你的浏览器直接发送到所选地图服务商，
            本平台不经手、不存储。未绑定时使用默认数据源。
          </div>
          {status && (
            <div style={{ color: status.tone === 'ok' ? 'var(--accent)' : 'var(--danger, #c0392b)', fontSize: 12 }}>
              {status.text}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

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

/**
 * HITL 确认卡片:RouteBot 在歧义地点/出行方式两个决策点暂停等人拍板。
 * 有选项渲染按钮组;同时保留自由输入(选项都不合适时用户可以自己说)。
 * busy 中禁点 —— 答复正在续跑,重复提交会产生两个方案。
 */
function ConfirmCard({ prompt, options, busy, onAnswer }) {
  const [text, setText] = useState('')
  const submitText = () => {
    const v = text.trim()
    if (!v || busy) return
    setText('')
    onAnswer(v)
  }
  return (
    <div className="msg msg-confirm">
      <div className="confirm-q">{prompt}</div>
      {options.length > 0 && (
        <div className="confirm-options">
          {options.map((opt) => (
            <button key={opt} className="confirm-opt" disabled={busy} onClick={() => onAnswer(opt)}>
              {opt}
            </button>
          ))}
        </div>
      )}
      <div className="confirm-free">
        <input
          type="text"
          value={text}
          disabled={busy}
          placeholder={options.length > 0 ? '都不合适？自己填一个' : '输入你的答复…'}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') submitText()
          }}
        />
        <button onClick={submitText} disabled={busy || !text.trim()}>确定</button>
      </div>
    </div>
  )
}

export function AgentPane({ onCollapse, onPlan }) {
  const { threads, busy, unavailable, streamText, thinkText, send, respond, clear } = useAgent({ onPlan })
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

      <MapSourceSettings />

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
          if (m.kind === 'confirm') {
            return (
              <ConfirmCard key={i} prompt={m.prompt} options={m.options} busy={busy}
                onAnswer={(answer) => respond(m.sessionId, answer)} />
            )
          }
          if (m.kind === 'exec') {
            // 委托执行卡片:留在对话里作为"这步用了你绑定的地图服务"的可见记录
            return (
              <div key={i} className="msg msg-confirm">
                {m.provider ? `已通过 ${m.provider} 执行：` : '地图服务执行：'}
                {m.hint}
              </div>
            )
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
