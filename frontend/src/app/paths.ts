// 路由路径常量：全站统一引用，避免各处硬编码字符串。
// 路由命名对齐旧版（第一期范围：官网 + 认证 + 商品 + 下单支付）。
export const paths = {
  home: '/',
  products: '/products',
  productDetail: (id: number | string) => `/products/${id}`,
  login: '/login',
  register: '/register',
  checkout: (productId: number | string) => `/checkout/${productId}`,
  payResult: '/pay/result',
} as const

/** 官网顶栏导航项。 */
export const siteNavItems = [
  { to: paths.home, label: '首页', end: true },
  { to: paths.products, label: '商品', end: false },
] as const
