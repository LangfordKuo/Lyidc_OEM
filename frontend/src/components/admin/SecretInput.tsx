import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { SECRET_KEEP, type SecretState } from '@/lib/secret'

/**
 * 密钥 / 口令的**三态**输入（契约 12.1，语义见 src/lib/secret.ts）：
 *   keep    —— 留空保持不变（请求体里省略该字段）；
 *   replace —— 输入新值，保存后替换；
 *   clear   —— 点「清空」显式置空（请求体里发空串）。
 * 接口只回显配置标志与掩码，页面任何地方都不保存明文。
 */
export default function SecretInput({
  label,
  name,
  configured,
  masked,
  state,
  onChange,
  placeholder,
  hint,
}: {
  label: string
  name: string
  configured: boolean
  /** 服务端回带的掩码（如 `abcd****wxyz`），未配置时为空串。 */
  masked: string
  state: SecretState
  onChange: (next: SecretState) => void
  placeholder?: string
  hint?: string
}) {
  return (
    <div className="space-y-2">
      <Field>
        <FieldLabel htmlFor={name}>{label}</FieldLabel>
        <Input
          id={name}
          name={name}
          type="password"
          autoComplete="new-password"
          value={state.value}
          disabled={state.mode === 'clear'}
          placeholder={
            state.mode === 'clear'
              ? '已标记清空（取消清空后可重新输入）'
              : (placeholder ??
                (configured ? `已配置（${masked}）· 留空保持不变` : '未配置 · 输入以设置'))
          }
          onChange={(event) =>
            onChange(event.target.value ? { mode: 'replace', value: event.target.value } : SECRET_KEEP)
          }
        />
      </Field>

      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="text-muted-foreground">
          当前状态：
          <span data-testid={`${name}-status`}>{configured ? `已配置（${masked}）` : '未配置'}</span>
        </span>
        {state.mode === 'replace' ? (
          <Badge variant="secondary" className="bg-accent text-primary">
            保存后替换
          </Badge>
        ) : null}
        {state.mode === 'clear' ? (
          <>
            <Badge variant="destructive">保存后清空</Badge>
            <Button size="xs" variant="ghost" onClick={() => onChange(SECRET_KEEP)}>
              取消清空
            </Button>
          </>
        ) : null}
        {configured && state.mode !== 'clear' ? (
          <Button
            size="xs"
            variant="ghost"
            onClick={() => onChange({ mode: 'clear', value: '' })}
          >
            清空{label}
          </Button>
        ) : null}
      </div>
      {hint ? <FieldDescription className="text-xs">{hint}</FieldDescription> : null}
    </div>
  )
}
