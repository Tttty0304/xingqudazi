import { useCallback, useEffect, useState } from 'react'
import { api, errorMessage } from '../api/client'
import { useAuth } from '../context/AuthContext'
import type { RecommendationCandidate } from '../types'
import './RecommendationsPage.css'

/**
 * AI 推荐页（本轮新增，对应 Task20 规则化匹配演示的前端闭环，此前
 * `POST /api/recommendations/generate`、`GET/PUT /api/recommendations` 均已实现
 * 并有真机验证，但前端完全没有消费方——用户永远看不到任何推荐结果，是最典型的
 * "后端能力完整、前端完全缺失"的半成品，本次补齐）。
 */
export function RecommendationsPage() {
  const { token } = useAuth()

  const [candidates, setCandidates] = useState<RecommendationCandidate[] | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [generating, setGenerating] = useState(false)
  const [generateHint, setGenerateHint] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [confirmedPeers, setConfirmedPeers] = useState<Set<string>>(new Set())
  const [addFriendHint, setAddFriendHint] = useState<Record<string, string>>({})

  const load = useCallback(() => {
    api
      .get<RecommendationCandidate[]>('/api/recommendations', token)
      .then(setCandidates)
      .catch((err) => setLoadError(errorMessage(err)))
  }, [token])

  useEffect(() => {
    load()
  }, [load])

  const handleGenerate = async () => {
    setGenerating(true)
    setGenerateHint(null)
    try {
      const result = await api.post<{ created: number }>('/api/recommendations/generate', undefined, token)
      setGenerateHint(result.created > 0 ? `生成了 ${result.created} 条新推荐` : '暂无新的推荐（可先去关注事项页设置关注关键词）')
      load()
    } catch (err) {
      setGenerateHint(errorMessage(err))
    } finally {
      setGenerating(false)
    }
  }

  const handleRespond = async (candidateId: string, peerId: string, action: 'confirm' | 'dismiss') => {
    setActionError(null)
    try {
      await api.put(`/api/recommendations/${candidateId}`, { action }, token)
      if (action === 'confirm') {
        setConfirmedPeers((prev) => new Set(prev).add(peerId))
      } else {
        setCandidates((prev) => prev?.filter((c) => c.candidate_id !== candidateId) ?? prev)
      }
    } catch (err) {
      setActionError(errorMessage(err))
    }
  }

  const handleAddFriend = async (peerId: string, candidateId: string) => {
    try {
      await api.post('/api/friends/requests', { target_user_id: peerId }, token)
      setAddFriendHint((prev) => ({ ...prev, [candidateId]: '已发送好友请求' }))
      setCandidates((prev) => prev?.filter((c) => c.candidate_id !== candidateId) ?? prev)
    } catch (err) {
      setAddFriendHint((prev) => ({ ...prev, [candidateId]: errorMessage(err) }))
    }
  }

  return (
    <div className="recommendations-page">
      <h2 className="recommendations-title">AI 推荐</h2>
      <p className="recommendations-hint">
        基于「关注事项」的规则化简单匹配演示：按关键词重合度 + 是否共处同一兴趣房间打分，
        非模型驱动，是预留的 AI-native 能力方向的最小可演示版本。
      </p>

      <div className="recommendations-generate-row">
        <button onClick={handleGenerate} disabled={generating}>
          {generating ? '生成中…' : '生成/刷新推荐'}
        </button>
        {generateHint && <span className="recommendations-generate-hint">{generateHint}</span>}
      </div>

      {loadError && <div className="recommendations-error">⚠️ 加载失败：{loadError}</div>}
      {actionError && <div className="recommendations-error">⚠️ {actionError}</div>}

      {candidates === null && <div className="recommendations-empty">加载中…</div>}
      {candidates !== null && candidates.length === 0 && (
        <div className="recommendations-empty">暂无推荐，点击上方按钮生成，或先去"关注事项"页设置关键词</div>
      )}

      <ul className="recommendations-list">
        {candidates?.map((c) => (
          <li key={c.candidate_id} className="recommendations-item">
            <div className="recommendations-item-header">
              <span className="recommendations-peer">{c.peer_username}</span>
              <span className="recommendations-score">匹配度 {c.match_score}</span>
            </div>
            <div className="recommendations-reason">{c.match_reason}</div>
            {confirmedPeers.has(c.peer_id) ? (
              <div className="recommendations-actions">
                <span className="recommendations-confirmed">已确认感兴趣</span>
                <button onClick={() => handleAddFriend(c.peer_id, c.candidate_id)}>加为好友</button>
                {addFriendHint[c.candidate_id] && (
                  <span className="recommendations-add-hint">{addFriendHint[c.candidate_id]}</span>
                )}
              </div>
            ) : (
              <div className="recommendations-actions">
                <button onClick={() => handleRespond(c.candidate_id, c.peer_id, 'confirm')}>感兴趣</button>
                <button
                  className="recommendations-dismiss"
                  onClick={() => handleRespond(c.candidate_id, c.peer_id, 'dismiss')}
                >
                  忽略
                </button>
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
