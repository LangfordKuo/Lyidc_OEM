import { useCallback, useEffect, useState } from 'react'
import { Button, Card } from '@heroui/react'

import { ApiError } from '../api/client'
import { fetchHealth, isHealthData, type HealthData } from '../api/health'
import { formatDateTime } from '../lib/format'
import StatusBadge from './StatusBadge'

type LoadState = 'loading' | 'ready' | 'failed'

// HealthPanel 实时请求 /api/v1/health 并展示后端与数据库状态。
export default function HealthPanel() {
  const [state, setState] = useState<LoadState>('loading')
  const [health, setHealth] = useState<HealthData | null>(null)
  const [error, setError] = useState('')

  const applySuccess = useCallback((data: HealthData) => {
    setHealth(data)
    setError('')
    setState('ready')
  }, [])

  const applyFailure = useCallback((err: unknown) => {
    // 后端降级（HTTP 503）时 data 仍带 db 状态，尽量保留展示
    if (err instanceof ApiError && isHealthData(err.data)) {
      setHealth(err.data)
    }
    setError(err instanceof Error ? err.message : '未知错误')
    setState('failed')
  }, [])

  // reload 供「重新检查」按钮调用。
  const reload = useCallback(async () => {
    setState('loading')
    setError('')
    try {
      applySuccess(await fetchHealth())
    } catch (err) {
      applyFailure(err)
    }
  }, [applySuccess, applyFailure])

  // 首次进入自动检查一次；结果在异步回调中写入状态，避免 effect 内同步 setState。
  useEffect(() => {
    let cancelled = false
    fetchHealth()
      .then((data) => {
        if (!cancelled) {
          applySuccess(data)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          applyFailure(err)
        }
      })
    return () => {
      cancelled = true
    }
  }, [applySuccess, applyFailure])

  const backendTone = state === 'ready' ? 'ok' : state === 'failed' ? 'error' : 'pending'
  const backendLabel = state === 'ready' ? '正常' : state === 'failed' ? '不可用' : '检查中'
  const dbTone = health ? (health.db === 'up' ? 'ok' : 'error') : 'pending'

  return (
    <Card>
      <Card.Header>
        <Card.Title>服务健康状态</Card.Title>
        <Card.Description>
          实时调用 GET /api/v1/health（开发环境经 Vite 代理转发到 127.0.0.1:8080）
        </Card.Description>
      </Card.Header>
      <Card.Content className="space-y-3">
        <div className="flex items-center justify-between gap-4">
          <span className="text-sm text-gray-600">后端服务</span>
          <StatusBadge tone={backendTone} label={backendLabel} />
        </div>
        <div className="flex items-center justify-between gap-4">
          <span className="text-sm text-gray-600">数据库（db）</span>
          <StatusBadge tone={dbTone} label={health ? health.db : '未知'} />
        </div>
        <div className="flex items-center justify-between gap-4">
          <span className="text-sm text-gray-600">响应时间（后端本地时间）</span>
          <span className="text-sm text-gray-900">
            {health?.time ? formatDateTime(health.time) : '—'}
          </span>
        </div>
        {error ? (
          <p role="alert" className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
            健康检查失败：{error}
          </p>
        ) : null}
      </Card.Content>
      <Card.Footer>
        <Button
          variant="secondary"
          size="sm"
          isPending={state === 'loading'}
          onPress={() => void reload()}
        >
          重新检查
        </Button>
      </Card.Footer>
    </Card>
  )
}
