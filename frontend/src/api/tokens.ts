import { localStore } from '../lib/storage'

// 会员 token 与管理端 token **分离存储**（契约 1.1：两类 token 用 aud 区分，互不通用），
// 键名不同，任一方的登录/登出都不会覆盖另一方。
const MEMBER_TOKEN_KEY = 'lyidc.member.token'
const MEMBER_PROFILE_KEY = 'lyidc.member.profile'
const ADMIN_TOKEN_KEY = 'lyidc.admin.token'
const ADMIN_PROFILE_KEY = 'lyidc.admin.profile'

// 会员 token：仅由 src/api/client.ts 注入到 `Authorization` 头，不写入任何日志。
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

// 会员资料缓存：仅用于首屏占位（真实数据一律以 GET /members/me 为准）。
export function getMemberProfile<T>(): T | null {
  return localStore.getJSON<T>(MEMBER_PROFILE_KEY)
}

export function setMemberProfile(profile: unknown): void {
  localStore.setJSON(MEMBER_PROFILE_KEY, profile)
}

// 管理端 token：管理后台（阶段 8）使用，与会员 token 完全隔离——两处登录互不覆盖。
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

// 管理员资料缓存：仅用于后台首屏占位（真实数据一律以 GET /admin/profile 为准）。
// 缓存里含角色（role），界面按角色矩阵渲染权限，不缓存任何凭据。
export function getAdminProfile<T>(): T | null {
  return localStore.getJSON<T>(ADMIN_PROFILE_KEY)
}

export function setAdminProfile(profile: unknown): void {
  localStore.setJSON(ADMIN_PROFILE_KEY, profile)
}
