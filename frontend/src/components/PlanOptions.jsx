// PlanOptions.jsx —— 规划选项 + 手动输入坐标
//
// 校验的"分层":
//   - 格式/范围错误 → 前端当场拦截,错误显示在具体字段下方(内联,非弹窗)
//   - 业务规则(点数上限、出行方式白名单) → 后端为准;前端抄一份做提示,
//     仅作体验优化,不是安全边界
//
// ── 模式选择用 radio 卡片而不是 checkbox ──
// 手动/自动是互斥的主模式,不是附加项:
//   1) 否定式复选框(勾上=手动)让"自动模式"在界面上没有可见入口
//   2) 默认值应落在最常用的路径上
// 控件类型跟随语义:二选一用 radio。

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
