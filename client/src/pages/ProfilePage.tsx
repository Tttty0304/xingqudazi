import { useEffect, useRef, useState, type ChangeEvent } from 'react'
import { api, errorMessage, resolveMediaUrl } from '../api/client'
import { useAuth } from '../context/AuthContext'
import './ProfilePage.css'

interface Profile {
  id: string
  username: string
  is_guest: boolean
  avatar_url: string
  bio: string
}

export function ProfilePage() {
  const { token } = useAuth()
  const [profile, setProfile] = useState<Profile | null>(null)
  const [bio, setBio] = useState('')
  const [avatarURL, setAvatarURL] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    api.get<Profile>('/api/me/profile', token)
      .then((data) => { setProfile(data); setBio(data.bio); setAvatarURL(data.avatar_url) })
      .catch((err) => setError(errorMessage(err)))
  }, [token])

  const save = async () => {
    setSaving(true)
    setError(null)
    try {
      const updated = await api.put<Profile>('/api/me/profile', { avatar_url: avatarURL, bio }, token)
      setProfile(updated)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setSaving(false)
    }
  }

  const uploadAvatar = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return
    setSaving(true)
    setError(null)
    try {
      const form = new FormData()
      form.append('file', file)
      const uploaded = await api.upload<{ url: string }>('/api/media/upload', form, token)
      setAvatarURL(uploaded.url)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setSaving(false)
    }
  }

  if (!profile) return <div className="profile-shell">{error ? `⚠️ ${error}` : '正在加载个人资料…'}</div>

  return <section className="profile-shell">
    <h2>个人资料</h2>
    <div className="profile-avatar">
      {avatarURL ? <img src={resolveMediaUrl(avatarURL)} alt="头像" /> : <span>{profile.username.slice(0, 1).toUpperCase()}</span>}
    </div>
    <p>账号：{profile.username}{profile.is_guest ? '（访客）' : ''}</p>
    <input ref={inputRef} type="file" accept="image/jpeg,image/png,image/gif,image/webp" hidden onChange={uploadAvatar} />
    <button type="button" onClick={() => inputRef.current?.click()} disabled={saving}>上传头像</button>
    <label>个人简介<textarea value={bio} onChange={(event) => setBio(event.target.value)} maxLength={280} placeholder="介绍一下你的兴趣吧" /></label>
    <button type="button" onClick={save} disabled={saving}>{saving ? '保存中…' : '保存资料'}</button>
    {error && <p className="profile-error">⚠️ {error}</p>}
  </section>
}
