// 密钥 / 口令的**三态**语义（契约 12.1）：后台设置里的密钥字段永不回显明文，
// 更新时：省略 = 保持不变、给值 = 替换、空串 = 清空。

export type SecretMode = 'keep' | 'replace' | 'clear'

export interface SecretState {
  mode: SecretMode
  value: string
}

/** 三态的初值：保持不变。 */
export const SECRET_KEEP: SecretState = { mode: 'keep', value: '' }

/** 把三态值转成请求体字段（keep 时返回 undefined，即不发送该键）。 */
export function secretPayload(state: SecretState): string | undefined {
  if (state.mode === 'keep') {
    return undefined
  }
  return state.mode === 'clear' ? '' : state.value
}
