// client.js —— 迷你 MCP 客户端(JSON-RPC over 两种传输)
//
// 只实现本应用需要的最小面:initialize → tools/list → tools/call。
// 两种传输:
//   streamable HTTP —— 单次 POST,响应可能是 JSON 或 SSE 帧(按 Content-Type 分流)
//   SSE —— GET 建立事件流拿 endpoint,再向 messages 地址 POST 请求,响应经流回传
//
// 会话密钥只在本模块与调用方之间流转,不出用户设备。

const RCP_TIMEOUT = 20000

function timeout(ms, label) {
  return new Promise((_, reject) =>
    setTimeout(() => reject(new Error(`${label} 超时(${Math.round(ms / 1000)}s)`)), ms)
  )
}

/** 解析 SSE 文本帧,取出与 id 匹配的 JSON-RPC 响应(取不到就取最后一个 result)。 */
function pickSseResponse(text, id) {
  const datas = text.split('\n').filter((l) => l.startsWith('data:'))
  let last = null
  for (const line of datas) {
    try {
      const json = JSON.parse(line.slice(5).trim())
      if (json.id === id) return json
      last = json
    } catch {
      // 非 JSON 数据帧(如 endpoint 事件),跳过
    }
  }
  return last
}

/** Streamable HTTP 传输的单次 POST;返回 {json, session}。 */
async function postHttp(url, body, session) {
  const res = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
      ...(session ? { 'Mcp-Session-Id': session } : {}),
    },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    throw new Error(`地图服务响应异常(HTTP ${res.status})`)
  }
  const sid = res.headers.get('mcp-session-id') || session
  const ct = res.headers.get('content-type') || ''
  const text = await res.text()
  if (!text.trim()) {
    // notification 等请求的响应可能为空体,无 JSON-RPC 回包可取
    return { json: null, session: sid }
  }
  let json
  if (ct.includes('text/event-stream')) {
    json = pickSseResponse(text, body.id)
  } else {
    json = JSON.parse(text)
  }
  if (!json) return { json: null, session: sid }
  if (json.error) {
    throw new Error(json.error.message || `地图服务返回错误(${json.error.code})`)
  }
  return { json, session: sid }
}

/** SSE 传输会话:建立流 → 拿 messages 地址 → POST 请求 → 从流里收响应。 */
async function withSseSession(url, fn) {
  const es = new EventSource(url)
  try {
    const messagesUrl = await new Promise((resolve, reject) => {
      es.addEventListener('endpoint', (e) => {
        try {
          resolve(new URL(e.data, url).toString())
        } catch (err) {
          reject(new Error('地图服务会话地址无效'))
        }
      })
      es.onerror = () => reject(new Error('地图服务连接失败'))
      timeout(RCP_TIMEOUT, '地图服务连接').catch(reject)
    })

    let seq = 0
    const pending = new Map()
    es.addEventListener('message', (e) => {
      try {
        const json = JSON.parse(e.data)
        const resolve = pending.get(json.id)
        if (resolve) {
          pending.delete(json.id)
          resolve(json)
        }
      } catch {
        // 忽略无法解析的帧
      }
    })

    const rpc = async (method, params) => {
      const id = ++seq
      const p = fetch(messagesUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ jsonrpc: '2.0', id, method, params }),
      }).then((res) => {
        if (!res.ok) throw new Error(`地图服务响应异常(HTTP ${res.status})`)
      })
      const resp = await Promise.race([
        new Promise((resolve, reject) => {
          pending.set(id, resolve)
          timeout(RCP_TIMEOUT, '地图服务调用').catch(reject)
        }),
        p.catch((err) => {
          pending.delete(id)
          throw err
        }),
      ])
      if (resp.error) {
        throw new Error(resp.error.message || `地图服务返回错误(${resp.error.code})`)
      }
      return resp
    }
    return await fn(rpc)
  } finally {
    es.close()
  }
}

/**
 * 调用一个 MCP 工具,返回 content[0].text 解析出的 JSON(解析失败回原文)。
 * @param {{endpoint:string, transport:'http'|'sse'}} ep
 */
export async function callTool(ep, name, args) {
  const call = async (rpc) => {
    await rpc('initialize', {
      protocolVersion: '2025-03-26',
      capabilities: {},
      clientInfo: { name: 'route-planner', version: '1.0' },
    })
    try {
      await rpc('notifications/initialized', {})
    } catch {
      // 无状态服务端(如高德)会拒绝通知方法,忽略 —— initialize 已成功即可
    }
    const resp = await rpc('tools/call', { name, arguments: args })
    const content = resp.result?.content?.[0]
    const text = content?.text ?? ''
    try {
      return { data: JSON.parse(text), raw: text }
    } catch {
      return { data: null, raw: text }
    }
  }
  if (ep.transport === 'sse') {
    return withSseSession(ep.endpoint, call)
  }
  return call(async (method, params) => {
    const { json } = await postHttp(ep.endpoint, { jsonrpc: '2.0', id: Date.now(), method, params })
    return json
  })
}

/** 工具名全集(绑定校验与别名解析用)。 */
export async function listToolNames(ep) {
  const resp = await (ep.transport === 'sse'
    ? withSseSession(ep.endpoint, async (rpc) => {
        await rpc('initialize', {
          protocolVersion: '2025-03-26',
          capabilities: {},
          clientInfo: { name: 'route-planner', version: '1.0' },
        })
        await rpc('notifications/initialized', {})
        return rpc('tools/list', {})
      })
    : (async () => {
        await postHttp(ep.endpoint, {
          jsonrpc: '2.0', id: 1, method: 'initialize',
          params: { protocolVersion: '2025-03-26', capabilities: {}, clientInfo: { name: 'route-planner', version: '1.0' } },
        })
        const { json } = await postHttp(ep.endpoint, { jsonrpc: '2.0', id: 2, method: 'tools/list', params: {} })
        return json
      })())
  const tools = resp?.result?.tools
  if (!Array.isArray(tools)) {
    throw new Error('地图服务未返回工具列表,请检查 Key 是否有效')
  }
  return tools.map((t) => t.name)
}
