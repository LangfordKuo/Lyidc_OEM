import { createBrowserRouter } from 'react-router-dom'

import { routes } from './routes'

/** 浏览器路由实例（应用入口使用；测试用 createMemoryRouter(routes) 复用同一套路由）。 */
export const router = createBrowserRouter(routes)
