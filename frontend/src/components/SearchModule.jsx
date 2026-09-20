// SearchModule.jsx —— 地名搜索 + 候选列表
//
// 这个组件覆盖三个模式:
//
// 1) 受控输入:input 的 value 来自 state,onChange 只负责 setState。
//    清空输入框就是 setQuery(''),不需要查 DOM。
//
// 2) 组件通信:本组件不知道"地点加到哪里去了",只调用父组件传入的
//    onPick(place) 回调(状态提升)。子组件负责交互,数据归属由父组件决定,
//    添加地点的逻辑在 App 里。
//
// 3) 异步操作管理:searching 这个 state 让按钮在请求期间禁用,
//    防止重复提交。

import { useRef, useState } from 'react'
import { searchPlaces } from '../api.js'
import { Icon } from './icons.jsx'
import { SectionHeader, TextField } from './ui.jsx'

export function SearchModule({ onPick, onError }) {
  const [query, setQuery] = useState('')
  // 城市默认填北京：和"默认地图视野是北京"保持一致。
  // 限定城市能让 POI 搜索准得多（"故宫"在全国范围内可能命中一堆同名地点）。
  const [city, setCity] = useState('北京')
  const [candidates, setCandidates] = useState([])
  const [searching, setSearching] = useState(false)

  // ref 只用于"添加完地点后把焦点还给输入框",不用于读值(受控组件的职责)。
  const inputRef = useRef(null)

  async function doSearch() {
    const q = query.trim()
    if (!q || searching) return

    setSearching(true)
    try {
      const data = await searchPlaces(q, city.trim())
      setCandidates(data.places || [])
    } catch (err) {
      // 错误不自行展示,交给父组件统一处理,避免每个组件维护一套错误 UI。
      onError(err.message)
      setCandidates([])
    } finally {
      // finally 保证无论成功失败都会解锁按钮,
      // 否则一次请求失败后按钮就永久禁用。
      setSearching(false)
    }
  }

  function pick(place) {
    onPick(place)
    // 选完清空：候选列表和输入框都归零，焦点回到输入框，方便连续添加。
    setQuery('')
    setCandidates([])
    inputRef.current?.focus() // ?. 可选链:ref 可能尚未挂载
  }

  return (
    <div className="module">
      <SectionHeader title="添加地点" hint="高德 POI 搜索" />

      <div style={{ display: 'flex', gap: 8 }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <TextField
            size="lg"
            icon="search"
            value={query}
            onChange={setQuery}
            onEnter={doSearch}
            inputRef={inputRef}
            placeholder="输入地名，如 故宫"
          />
        </div>
        <button
          className="btn btn--primary"
          style={{ height: 38, borderRadius: 19 }}
          onClick={doSearch}
          disabled={searching}
        >
          {searching ? '搜索中…' : '搜索'}
        </button>
      </div>

      <TextField
        value={city}
        onChange={setCity}
        onEnter={doSearch}
        icon="pin"
        placeholder="城市，如 北京"
        suffix="留空 = 全国"
      />

      {/* 条件渲染：没候选就什么都不渲染。
          React 里 {cond && <X/>} 是最常见的写法，cond 为 false 时
          渲染的是 false —— React 知道"false 不渲染任何东西"。 */}
      {candidates.length > 0 && (
        <ul className="candidates">
          {candidates.map((place, i) => (
            <li
              // key 用"内容特征"而不是下标：候选列表每次搜索都换一批，
              // 用下标当 key 会让 React 把新旧候选错认成同一个东西。
              key={`${place.name}-${place.lat}-${place.lng}`}
              className={`candidate${i === 0 ? ' is-active' : ''}`}
              onClick={() => pick(place)}
            >
              <Icon name="pin" size={14} style={{ color: 'var(--text-secondary)', flexShrink: 0 }} />
              <div className="candidate-text">
                <span className="candidate-name">{place.name}</span>
                <span className="candidate-addr">{place.address || '—'}</span>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
