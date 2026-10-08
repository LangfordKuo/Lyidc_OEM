import { localStore } from '../lib/storage'

// 会员 token 的存储键（契约硬性约定：登录后写入 localStorage 的该键）。
const MEMBER_TOKEN_KEY = 'lyidc.member.token'
// 会员资料缓存：仅用于首屏占位（真实数据一律以 GET /members/me 为准）。
const MEMBER_PROFILE_KEY = 'lyidc.member.profile'

/** 会员 token：仅由 src/api/client.ts 注入到 `Authorization` 头，不写入任何日志。 */
export function getMemberToken(): string | null {
  return localStore.get(MEMBER_TOKEN_KEY)
}

export function setMemberToken(token: string): void {
  localStore.set(MEMBER_TOKEN_KEY, token)
}

export function clearMemberToken(): void {
  localStore.remove(MEMBER_TOKEN_KEY)
  localStore.remove(MEMBER_PROFILE_KEY)
}

export function getMemberProfile<T>(): T | null {
  return localStore.getJSON<T>(MEMBER_PROFILE_KEY)
}

export function setMemberProfile(profile: unknown): void {
  localStore.setJSON(MEMBER_PROFILE_KEY, profile)
}
