// 浏览器存储的安全封装：隐私模式、禁用存储或非浏览器环境下读写失败不抛错。
type StorageKind = 'local' | 'session'

function pick(kind: StorageKind): Storage | null {
  if (typeof window === 'undefined') {
    return null
  }
  try {
    return kind === 'local' ? window.localStorage : window.sessionStorage
  } catch {
    return null
  }
}

export interface StorageAdapter {
  get(key: string): string | null
  set(key: string, value: string): void
  remove(key: string): void
  getJSON<T>(key: string): T | null
  setJSON(key: string, value: unknown): void
}

function createAdapter(kind: StorageKind): StorageAdapter {
  return {
    get(key) {
      try {
        return pick(kind)?.getItem(key) ?? null
      } catch {
        return null
      }
    },
    set(key, value) {
      try {
        pick(kind)?.setItem(key, value)
      } catch {
        // 存储不可用（隐私模式/超配额）时静默降级为「不持久化」
      }
    },
    remove(key) {
      try {
        pick(kind)?.removeItem(key)
      } catch {
        // 同上
      }
    },
    getJSON<T>(key: string): T | null {
      const raw = this.get(key)
      if (!raw) {
        return null
      }
      try {
        return JSON.parse(raw) as T
      } catch {
        this.remove(key)
        return null
      }
    },
    setJSON(key, value) {
      try {
        this.set(key, JSON.stringify(value))
      } catch {
        // 循环引用等序列化失败：不落存储
      }
    },
  }
}

// 持久存储：会员 token、会员资料缓存。
export const localStore = createAdapter('local')
// 会话存储：下单草稿、最近下单的订单号（关掉标签页即失效）。
export const sessionStore = createAdapter('session')
