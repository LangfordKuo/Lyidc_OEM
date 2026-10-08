import { useState } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { zodResolver } from '@hookform/resolvers/zod'
import { AlertCircleIcon, LoaderCircleIcon } from 'lucide-react'
import { useForm } from 'react-hook-form'

import { errorMessage } from '@/api/client'
import { useAuth } from '@/auth/authContext'
import AuthShell from '@/components/layout/AuthShell'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { buildLoginUrl, safeRedirect } from '@/lib/redirect'
import { registerSchema, type RegisterForm } from '@/lib/validate'

/**
 * 会员注册页：字段规则严格对齐契约 6.2（用户名 3-32 位、邮箱 ≤128 字节、密码 8-72 字节），
 * 注册成功后自动登录并回跳 redirect。
 */
export default function Register() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const { register: registerMember, isAuthenticated, initializing } = useAuth()

  const redirectTarget = safeRedirect(searchParams.get('redirect'))
  const [serverError, setServerError] = useState('')

  const form = useForm<RegisterForm>({
    resolver: zodResolver(registerSchema),
    defaultValues: { username: '', email: '', password: '', confirm: '' },
  })

  if (isAuthenticated && !initializing) {
    return <Navigate to={redirectTarget} replace />
  }

  const onSubmit = form.handleSubmit(async (values) => {
    setServerError('')
    try {
      await registerMember({
        username: values.username.trim(),
        email: values.email.trim(),
        password: values.password,
      })
      navigate(redirectTarget, { replace: true })
    } catch (error) {
      setServerError(errorMessage(error, '注册失败，请稍后重试'))
    }
  })

  return (
    <AuthShell
      title="注册会员账号"
      description="注册后即可下单、支付并提交工单"
      footer={
        <>
          已有账号？
          <Link
            className="ml-1 font-medium text-primary hover:underline"
            to={buildLoginUrl(searchParams.get('redirect') ?? '')}
          >
            去登录
          </Link>
        </>
      }
    >
      <form className="space-y-5" onSubmit={onSubmit} noValidate aria-label="注册表单">
        {serverError ? (
          <Alert variant="destructive">
            <AlertCircleIcon aria-hidden />
            <AlertDescription>{serverError}</AlertDescription>
          </Alert>
        ) : null}

        <Field data-invalid={form.formState.errors.username ? true : undefined}>
          <FieldLabel htmlFor="register-username">用户名</FieldLabel>
          <Input
            id="register-username"
            autoComplete="username"
            placeholder="3-32 位字母、数字或下划线"
            autoFocus
            aria-invalid={form.formState.errors.username ? true : undefined}
            {...form.register('username')}
          />
          <FieldError errors={[form.formState.errors.username]} />
        </Field>

        <Field data-invalid={form.formState.errors.email ? true : undefined}>
          <FieldLabel htmlFor="register-email">邮箱</FieldLabel>
          <Input
            id="register-email"
            type="email"
            autoComplete="email"
            placeholder="用于接收订单与到期通知"
            aria-invalid={form.formState.errors.email ? true : undefined}
            {...form.register('email')}
          />
          <FieldError errors={[form.formState.errors.email]} />
        </Field>

        <Field data-invalid={form.formState.errors.password ? true : undefined}>
          <FieldLabel htmlFor="register-password">密码</FieldLabel>
          <Input
            id="register-password"
            type="password"
            autoComplete="new-password"
            placeholder="至少 8 个字节（约 8 个字符）"
            aria-invalid={form.formState.errors.password ? true : undefined}
            {...form.register('password')}
          />
          <FieldError errors={[form.formState.errors.password]} />
        </Field>

        <Field data-invalid={form.formState.errors.confirm ? true : undefined}>
          <FieldLabel htmlFor="register-confirm">确认密码</FieldLabel>
          <Input
            id="register-confirm"
            type="password"
            autoComplete="new-password"
            placeholder="请再次输入密码"
            aria-invalid={form.formState.errors.confirm ? true : undefined}
            {...form.register('confirm')}
          />
          <FieldError errors={[form.formState.errors.confirm]} />
          <FieldDescription>用户名与邮箱唯一且大小写不敏感。</FieldDescription>
        </Field>

        <Button type="submit" className="h-10 w-full text-base" disabled={form.formState.isSubmitting}>
          {form.formState.isSubmitting ? (
            <>
              <LoaderCircleIcon className="animate-spin" data-icon="inline-start" aria-hidden />
              注册中…
            </>
          ) : (
            '注册并登录'
          )}
        </Button>

        <p className="text-center text-xs text-muted-foreground">
          注册即表示同意本站服务条款。
        </p>
      </form>
    </AuthShell>
  )
}
