import { http } from './client'
import type { LoginResult, Member, RegisterInput } from './types'

// 会员端账号接口（契约 6.2）。
// 登录成功后由调用方（src/auth/AuthContext.tsx）负责落 token，api 层不写存储。

/** POST /api/v1/auth/register —— 注册会员，返回新增的会员对象。 */
export function registerMember(input: RegisterInput): Promise<Member> {
  return http.post<Member>('/auth/register', input)
}

/** POST /api/v1/auth/login —— 会员登录，返回 {token, expires_at, member}。 */
export function loginMember(username: string, password: string): Promise<LoginResult> {
  return http.post<LoginResult>('/auth/login', { username, password })
}

/** GET /api/v1/members/me —— 当前登录会员（会员 token）。 */
export function fetchMe(): Promise<Member> {
  return http.get<Member>('/members/me', { auth: 'member' })
}
