import HealthPanel from '../components/HealthPanel'

// Home 是阶段 0 的最小页面：脚手架就绪提示 + 后端健康状态。
export default function Home() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-gray-50 p-6">
      <div className="w-full max-w-2xl space-y-6">
        <header className="space-y-1">
          <h1 className="text-2xl font-semibold text-gray-900">Lyidc_OEM 脚手架就绪</h1>
          <p className="text-sm text-gray-500">
            岭云互联 IDC 财务系统 · 阶段 0（Go + Gin 后端 / React + HeroUI 前端）
          </p>
        </header>
        <HealthPanel />
      </div>
    </main>
  )
}
