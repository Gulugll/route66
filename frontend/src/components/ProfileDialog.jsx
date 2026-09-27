// ProfileDialog.jsx —— 账号资料编辑弹层(社区身份层)
//
// 职责单一:昵称/简介的表单校验 + 头像上传 + 调 api 落库。
// 成功后通过 onSaved(patch) 通知上层同步 user 缓存(useAuth.updateUser),
// 自己不持有任何全局状态 —— 弹层是"收集输入"的,不是"保管数据"的。
//
// 昵称为空是合法值:后端存空串,展示层回退 username(见 TopBar)。
// 与后端校验同款规则在提交前先拦一遍 —— 只是体验优化,后端那份才是边界。

import { useEffect, useRef, useState } from 'react'
import { updateProfile, uploadAvatar } from '../api.js'

export function ProfileDialog({ user, onClose, onSaved, onError }) {
  const [nickname, setNickname] = useState(user.nickname || '')
  const [bio, setBio] = useState(user.bio || '')
  const [avatarUrl, setAvatarUrl] = useState(user.avatar_url || '')
  const [saving, setSaving] = useState(false)
  const [uploading, setUploading] = useState(false)
  const fileRef = useRef(null)

  // Esc 关弹层 —— 与旧设置弹层同一个交互约定
  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const handlePickFile = () => fileRef.current?.click()

  const handleFileChange = async (e) => {
    const file = e.target.files?.[0]
    e.target.value = '' // 允许同一文件连续选两次
    if (!file) return
    if (file.size > 10 * 1024 * 1024) {
      onError('头像不能超过 10MB')
      return
    }
    setUploading(true)
    try {
      const data = await uploadAvatar(file)
      setAvatarUrl(data.avatar_url)
      onSaved({ avatar_url: data.avatar_url })
    } catch (err) {
      onError(err.message)
    } finally {
      setUploading(false)
    }
  }

  const handleSave = async () => {
    if (nickname.length > 32) {
      onError('昵称最多 32 个字符')
      return
    }
    if (bio.length > 200) {
      onError('简介最多 200 个字符')
      return
    }
    setSaving(true)
    try {
      await updateProfile(nickname.trim(), bio.trim())
      onSaved({ nickname: nickname.trim(), bio: bio.trim() })
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
        <div className="dialog-title">账号资料</div>
        <div className="dialog-hint">
          昵称和简介会显示在社区笔记里；留空则展示用户名 {user.username}。
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <Avatar avatarUrl={avatarUrl} username={user.username} size={56} />
          <div>
            <button className="btn-ghost" onClick={handlePickFile} disabled={uploading}>
              {uploading ? '上传中…' : '更换头像'}
            </button>
            <div style={{ fontSize: 11, color: 'var(--text-secondary)', marginTop: 4 }}>
              支持 jpg/png/webp，不超过 10MB
            </div>
          </div>
          <input
            ref={fileRef}
            type="file"
            accept=".jpg,.jpeg,.png,.webp,image/jpeg,image/png,image/webp"
            style={{ display: 'none' }}
            onChange={handleFileChange}
          />
        </div>

        <div className="field">
          <label className="field-label" htmlFor="pf-nickname">昵称</label>
          <div className="input-wrap">
            <input
              id="pf-nickname"
              className="input"
              value={nickname}
              maxLength={64}
              placeholder={user.username}
              onChange={(e) => setNickname(e.target.value)}
            />
          </div>
        </div>

        <div className="field">
          <label className="field-label" htmlFor="pf-bio">简介</label>
          <div className="input-wrap">
            <input
              id="pf-bio"
              className="input"
              value={bio}
              maxLength={256}
              placeholder="一句话介绍自己"
              onChange={(e) => setBio(e.target.value)}
            />
          </div>
        </div>

        <div className="dialog-actions">
          <button className="btn-ghost" onClick={onClose}>取消</button>
          <button className="btn-primary" onClick={handleSave} disabled={saving}>
            {saving ? '保存中…' : '保存'}
          </button>
        </div>
      </div>
    </div>
  )
}

// Avatar 头像:有图显示图,无图显示用户名首字符的墨色圆块。
// 独立成小组件是因为 M2 的社区卡片、评论列表都会复用它。
export function Avatar({ avatarUrl, username, size = 32 }) {
  const style = {
    width: size,
    height: size,
    borderRadius: '50%',
    flex: 'none',
    overflow: 'hidden',
    background: 'var(--ink)',
    color: '#fff',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: size * 0.42,
    fontWeight: 500,
  }
  if (avatarUrl) {
    return <img src={avatarUrl} alt={username} style={{ ...style, objectFit: 'cover' }} />
  }
  return <span style={style}>{(username || '?').slice(0, 1).toUpperCase()}</span>
}
