import { useCallback, useEffect, useRef, useState } from 'react'

import { errorMessage } from '../api/client'

// useAsync 是轻量的数据加载 hook（不引入 React Query 等重型依赖）：
// 负责加载态/错误态/竞态取消与手动重试，页面只需关心 loader 与依赖。
export interface AsyncState<T> {
  data: T | null
  loading: boolean
  error: string
  reload: () => void
}

export function useAsync<T>(
  loader: () => Promise<T>,
  deps: readonly unknown[],
  enabled = true,
): AsyncState<T> {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(enabled)
  const [error, setError] = useState('')
  const [tick, setTick] = useState(0)

  // loader 每次渲染都可能是新函数，放 ref 里避免 effect 反复触发。
  const loaderRef = useRef(loader)
  loaderRef.current = loader

  useEffect(() => {
    if (!enabled) {
      setLoading(false)
      return
    }
    let cancelled = false
    setLoading(true)
    setError('')
    loaderRef
      .current()
      .then((result) => {
        if (!cancelled) {
          setData(result)
          setLoading(false)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(errorMessage(err))
          setLoading(false)
        }
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, enabled, tick])

  return { data, loading, error, reload: useCallback(() => setTick((value) => value + 1), []) }
}
