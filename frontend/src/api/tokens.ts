import { localStore } from '../lib/storage'

// 会员 token 的存储键（契约硬性约定：登录后写入 localStorage 的该键）。
const MEMBER_TOKEN_KEY = 'lyidc.member.token'
// 会员资料缓存：仅用于首屏占位（真实数据一律以 GET /members/me 为准）。
const MEMBER_PROFILE_KEY = 'lyidc.member.profile'
// 管理端 token 与管理员资料缓存：键名与会员侧**分离**（契约 1.1：两类 token 用 aud 区分，
// 互不通用），任一方的登录/登出都不会覆盖另一方。
const ADMIN_TOKEN_KEY = 'lyidc.admin.token'
const ADMIN_PROFILE_KEY = 'lyidc.admin.profile'

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

// ---------------------------------------------------------------------------
// 管理端（阶段 8/9 后台）
// ---------------------------------------------------------------------------

/** 管理端 token：仅由 src/api/client.ts 在 auth='admin' 时注入 Authorization 头。 */
export function getAdminToken(): string | null {
  return localStore.get(ADMIN_TOKEN_KEY)
}

export function setAdminToken(token: string): void {
  localStore.set(ADMIN_TOKEN_KEY, token)
}

export function clearAdminToken(): void {
  localStore.remove(ADMIN_TOKEN_KEY)
  localStore.remove(ADMIN_PROFILE_KEY)
}

/** 管理员资料缓存（含 role）：仅用于后台首屏占位，不缓存任何凭据。 */
export function getAdminProfile<T>(): T | null {
  return localStore.getJSON<T>(ADMIN_PROFILE_KEY)
}

export function setAdminProfile(profile: unknown): void {
  localStore.setJSON(ADMIN_PROFILE_KEY, profile)
}
