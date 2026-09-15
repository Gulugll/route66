// SearchModule.jsx —— 地名搜索 + 候选列表
//
// 这个组件演示三件事：
//
// 1) **受控输入**：input 的 value 来自 state，onChange 只负责 setState。
//    好处是"想清空输入框"就是 setQuery('')，不需要去 DOM 里找那个 input。
//    手写版是 $('searchInput').value = '' —— 得先知道元素在哪。
//
// 2) **组件之间怎么通信**：这个组件不知道"地点加到哪里去了"。
//    它只调用父组件传进来的 onPick(place) 回调。
//    这叫"状态提升"：子组件负责交互，数据归谁管由父组件决定。
//    所以这个组件里你找不到任何"添加地点"的逻辑 —— 那是 App 的事。
//
// 3) **异步操作在组件里怎么管**：searching 这个 state 让按钮在请求期间
//    变成禁用态。没有它，用户会连点五次，发出五个请求。

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

  // 用 ref 拿到真实的 input 元素 —— 唯一目的是"添加完地点后把焦点还给它"。
  // 注意：我们没有用 ref 去读它的值（那是受控组件该做的事），
  // 只用它调了一次 .focus()。ref 是"逃生舱"，该用的时候不别扭，但别拿它当状态用。
  const inputRef = useRef(null)

  async function doSearch() {
    const q = query.trim()
    if (!q || searching) return

    setSearching(true)
    try {
      const data = await searchPlaces(q, city.trim())
      setCandidates(data.places || [])
    } catch (err) {
      // 错误不自己弹 alert，而是交给父组件统一展示。
      // 组件不该决定"错误长什么样" —— 那样每个组件都要维护一套错误 UI。
      onError(err.message)
      setCandidates([])
    } finally {
      // finally 保证无论成功失败都会解锁按钮。
      // 漏了这句，一次请求失败按钮就永久禁用了 —— 手写版很容易犯这个错。
      setSearching(false)
    }
  }

  function pick(place) {
    onPick(place)
    // 选完清空：候选列表和输入框都归零，焦点回到输入框，方便连续添加。
    setQuery('')
    setCandidates([])
    inputRef.current?.focus() // ?. 是可选链：ref 可能还没挂上，别炸
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
