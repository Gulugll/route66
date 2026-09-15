// PlanOptions.jsx —— 规划选项 + 手动输入坐标
//
// 校验逻辑放在这里，而不是丢给后端或 alert()。
//
// 手写版手动添加长这样：
//     if (Number.isNaN(lat) || Number.isNaN(lng)) { alert('纬度/经度要填数字'); return; }
// 两个问题：
//   1) alert 打断流程，而且是浏览器给的样式，和产品无关
//   2) 只说"要填数字"，不说是哪个字段错了、错在哪
//
// 这里改成：错误存进 state，渲染到具体字段下面。
// 注意校验的"分层"：
//   - 格式/范围错了 → 前端当场拦（用户马上能看到，不用等一个来回）
//   - 业务规则（点数上限、出行方式白名单）→ 后端说了算（前端也抄一份做提示，
//     但**不作为最终依据** —— 前端校验永远只是体验优化，不是安全边界）
//
// ── 模式选择为什么从 Checkbox 换成了两个 radio 卡片 ──
// 以前的写法是一个"反向 checkbox"：勾上 = 手动模式，不勾 = 自动模式。
// 三个问题叠在一起，结果就是用户（包括提问的作者本人）找不到"自动模式"：
//   1) 语义反着来：界面上根本没有"自动模式"四个字，只有一个否定式复选框
//   2) 默认值还是 manual=true（勾着），打开页面永远停在手动档
//   3) checkbox 暗示"可勾可不勾的附加项"，但这是二选一的主模式
// 控件类型要跟语义走：二选一用 radio；默认值放最常用的路径上。

import { useState } from 'react'
import { Icon } from './icons.jsx'
import { TextField } from './ui.jsx'

export function PlanOptions({ manual, onManualChange, pointCount, onAddManual }) {
  const [name, setName] = useState('')
  const [lat, setLat] = useState('')
  const [lng, setLng] = useState('')
  const [errors, setErrors] = useState({})

  function submit() {
    const next = {}
    const latN = Number(lat)
    const lngN = Number(lng)

    if (!lat.trim()) next.lat = '纬度不能为空'
    else if (!Number.isFinite(latN)) next.lat = '纬度必须是数字'
    else if (latN < -90 || latN > 90) next.lat = '纬度范围是 -90 ~ 90'

    if (!lng.trim()) next.lng = '经度不能为空'
    else if (!Number.isFinite(lngN)) next.lng = '经度必须是数字'
    else if (lngN < -180 || lngN > 180) next.lng = '经度范围是 -180 ~ 180'

    setErrors(next)
    if (Object.keys(next).length > 0) return // 有错就停在这，不往下走

    onAddManual({
      name: name.trim() || `地点${pointCount + 1}`,
      lat: latN,
      lng: lngN,
    })
    setName('')
    setLat('')
    setLng('')
    setErrors({})
  }

  return (
    <>
      {/* role="radiogroup" 让读屏软件把这组控件播报成"单选组"——
          真的 radio input 在内部,视觉是自绘的卡片。
          "自绘视觉 + 原生 input"是标准组合:样式自由,键盘/读屏不丢。 */}
      <div className="options-card" role="radiogroup" aria-label="规划方式">
        <label className={`mode-option ${!manual ? 'is-checked' : ''}`}>
          <input
            className="sr-only"
            type="radio"
            name="plan-mode"
            checked={!manual}
            onChange={() => onManualChange(false)}
          />
          <span className="mode-radio" aria-hidden="true" />
          <span className="mode-text">
            <strong>自动规划最短顺序</strong>
            <span className="mode-desc">算法（模拟退火）重排访问顺序，每段统一按驾车算</span>
          </span>
        </label>

        <label className={`mode-option ${manual ? 'is-checked' : ''}`}>
          <input
            className="sr-only"
            type="radio"
            name="plan-mode"
            checked={manual}
            onChange={() => onManualChange(true)}
          />
          <span className="mode-radio" aria-hidden="true" />
          <span className="mode-text">
            <strong>按我的列表顺序</strong>
            <span className="mode-desc">不重排顺序，每段可以单独选出行方式</span>
          </span>
        </label>
      </div>

      {/* 用原生 <details> 做折叠。
          它的开合状态由浏览器自己管，React 不介入 —— 有些状态
          根本不需要进 state，能用平台能力就别自己造。
          （真需要在 JS 里读"展开没有"，再改成受控。） */}
      <details className="details">
        <summary>
          <Icon name="chevronRight" size={12} className="chevron" />
          或手动输入坐标（高级）
        </summary>

        <div className="details-body">
          <TextField
            value={name}
            onChange={setName}
            placeholder={`名称（默认 地点${pointCount + 1}）`}
          />
          <TextField
            value={lat}
            onChange={setLat}
            onEnter={submit}
            placeholder="纬度，如 39.9098"
            error={errors.lat}
          />
          <TextField
            value={lng}
            onChange={setLng}
            onEnter={submit}
            placeholder="经度，如 116.3975"
            error={errors.lng}
          />
          <button className="btn btn--primary" onClick={submit}>
            <Icon name="plus" size={13} />
            添加
          </button>
        </div>
      </details>
    </>
  )
}
