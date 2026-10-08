import { createBrowserRouter } from 'react-router-dom'

import { routes } from './routes'

// 生产环境的 Browser Router 实例（App 默认使用）。测试用 createMemoryRouter(routes) 复用同一套路由。
export const router = createBrowserRouter(routes)
