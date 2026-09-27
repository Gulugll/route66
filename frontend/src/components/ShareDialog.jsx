// ShareDialog.jsx —— 「分享到社区」弹层。
//
// 把当前规划状态序列化成 plan_snapshot 发布。快照结构与后端
// community.validateSnapshot 的白名单一一对应(version 由后端规范化补齐,前端不传):
//   { mode, points: [{ name, lat, lng, legMode }] }
//
// mode 字段的语义在手动模式下有个小别扭:手动规划没有"全局方式",
// 这里以第一段的方式占位 —— 导入方只用 points(每点自带 legMode)重建路线,
// mode 只作为展示性元数据,不参与导入逻辑(见 App.handleImport)。

import { useRef, useState } from 'react'
import { publishPost, uploadPostImage } from '../api.js'

// MAX_IMAGES 一篇笔记的图片上限(与后端 validateImageURLs 的 9 一致)。
const MAX_IMAGES = 9

export function ShareDialog({ points, manual, mode, onClose, onPublished, onError }) {
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [saving, setSaving] = useState(false)
  // images: [{url, uploading}] —— 选图后立即上传,发布时只提交已完成的 URL。
  // "先传图后发帖"与后端契约一致:发布接口只收 URL,不收文件。
  const [images, setImages] = useState([])
  const fileRef = useRef(null)

  const uploadingCount = images.filter((i) => i.uploading).length
  // 有图还在传 → 不能发布(否则帖子会缺图)
  const imagesReady = uploadingCount === 0

  const handlePickFiles = () => fileRef.current?.click()

  const handleFilesChange = async (e) => {
    const files = [...(e.target.files || [])]
    e.target.value = ''
    if (!files.length) return
    const room = MAX_IMAGES - images.length
    if (files.length > room) {
      onError(`最多 ${MAX_IMAGES} 张,还能再加 ${room} 张`)
      return
    }
    // 占位先行:每张图立刻出现在九宫格里(灰块),传完换真图 —— 可感知的进度
    const placeholders = files.map(() => ({ url: '', uploading: true }))
    setImages((prev) => [...prev, ...placeholders])
    const startIdx = images.length
    files.forEach((file, i) => {
      uploadPostImage(file)
        .then((data) => {
          setImages((prev) =>
            prev.map((img, idx) => (idx === startIdx + i ? { url: data.url, uploading: false } : img))
          )
        })
        .catch((err) => {
          // 单张失败只剔除自己,不连坐其它图
          setImages((prev) => prev.filter((_, idx) => idx !== startIdx + i))
          onError(err.message)
        })
    })
  }

  const removeImage = (idx) => setImages((prev) => prev.filter((_, i) => i !== idx))

  const handleSubmit = async () => {
    if (title.trim().length > 30) {
      onError('标题最多 30 个字')
      return
    }
    if (content.trim().length > 2000) {
      onError('正文最多 2000 个字')
      return
    }
    if (!imagesReady) {
      onError('图片还在上传,稍等一下')
      return
    }
    setSaving(true)
    try {
      const snapshot = {
        mode: manual ? points[0]?.legMode || 'driving' : mode,
        points: points.map((p) => ({ name: p.name, lat: p.lat, lng: p.lng, legMode: p.legMode })),
      }
      const urls = images.map((i) => i.url).filter(Boolean)
      const data = await publishPost(title.trim(), content.trim(), snapshot, urls)
      onPublished(data.post)
      onClose()
    } catch (err) {
      onError(err.message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="overlay" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog">
        <div className="dialog-title">分享到社区</div>
        <div className="dialog-hint">
          将当前路线({points.length} 个点)发布为路线笔记,其他人可以一键按你的路线开始规划。
        </div>

        <div className="field">
          <label className="field-label" htmlFor="sh-title">标题</label>
          <div className="input-wrap">
            <input
              id="sh-title"
              className="input"
              value={title}
              maxLength={64}
              placeholder="一句话推荐这条路线(必填)"
              onChange={(e) => setTitle(e.target.value)}
            />
          </div>
        </div>

        <div className="field">
          <label className="field-label" htmlFor="sh-content">正文</label>
          <div className="input-wrap">
            <input
              id="sh-content"
              className="input"
              value={content}
              maxLength={2200}
              placeholder="玩法、注意事项、好吃好玩的…(必填)"
              onChange={(e) => setContent(e.target.value)}
            />
          </div>
        </div>

        <div className="community-share-points">
          {points.map((p, i) => (
            <span key={p.id}>
              {i > 0 && <span className="share-arrow">→</span>}
              {p.name}
            </span>
          ))}
        </div>

        <div className="field">
          <span className="field-label">图片（{images.length}/{MAX_IMAGES}）</span>
          <div className="share-images">
            {images.map((img, i) => (
              <div key={i} className={`share-img${img.uploading ? ' uploading' : ''}`}>
                {img.uploading ? (
                  <span className="share-img-loading">…</span>
                ) : (
                  <>
                    <img src={img.url} alt={`图片 ${i + 1}`} />
                    <button
                      type="button"
                      className="share-img-del"
                      onClick={() => removeImage(i)}
                      aria-label="删除图片"
                    >
                      ×
                    </button>
                  </>
                )}
              </div>
            ))}
            {images.length < MAX_IMAGES && (
              <button type="button" className="share-img-add" onClick={handlePickFiles}>
                ＋
              </button>
            )}
          </div>
          <input
            ref={fileRef}
            type="file"
            accept=".jpg,.jpeg,.png,.webp,image/jpeg,image/png,image/webp"
            multiple
            style={{ display: 'none' }}
            onChange={handleFilesChange}
          />
        </div>

        <div className="dialog-actions">
          <button className="btn-ghost" onClick={onClose}>取消</button>
          <button
            className="btn-primary"
            onClick={handleSubmit}
            disabled={saving || !imagesReady || !title.trim() || !content.trim()}
          >
            {saving ? '发布中…' : uploadingCount ? `上传中(${uploadingCount})` : '发布'}
          </button>
        </div>
      </div>
    </div>
  )
}
