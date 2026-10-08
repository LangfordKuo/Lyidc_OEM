import { isBillingCycle, type BillingCycle } from './cycles'
import { sessionStore } from './storage'

// 下单草稿：商品详情页选好的「周期 + 配置项」在跳转下单页/登录回跳后仍然可用。
// 存 sessionStorage（关标签页即失效），键里带 product_id 以免不同商品互相覆盖。
const DRAFT_PREFIX = 'lyidc.checkout.draft.'
const LAST_ORDER_KEY = 'lyidc.checkout.lastOrder'

export interface CheckoutDraft {
  productId: number
  cycle: BillingCycle
  /** 配置项 id → 所选值 id（字符串形式，与契约 12.4 的 config 一致） */
  config: Record<string, string>
  productName: string
}

function draftKey(productId: number): string {
  return `${DRAFT_PREFIX}${productId}`
}

export function saveCheckoutDraft(draft: CheckoutDraft): void {
  sessionStore.setJSON(draftKey(draft.productId), draft)
}

export function readCheckoutDraft(productId: number): CheckoutDraft | null {
  const raw = sessionStore.getJSON<Partial<CheckoutDraft>>(draftKey(productId))
  if (!raw || raw.productId !== productId || !isBillingCycle(raw.cycle)) {
    return null
  }
  return {
    productId,
    cycle: raw.cycle,
    config: raw.config ?? {},
    productName: typeof raw.productName === 'string' ? raw.productName : '',
  }
}

export function clearCheckoutDraft(productId: number): void {
  sessionStore.remove(draftKey(productId))
}

// ---------------------------------------------------------------------------
// 最近一次下单：支付结果页在渠道回跳参数缺失时据此找回订单。
// 注意：只记订单号与业务 ID，不含任何凭据。
// ---------------------------------------------------------------------------
export interface RememberedOrder {
  id: number
  trade_no: string
  productId: number
  createdAt: string
}

export function rememberOrder(order: Omit<RememberedOrder, 'createdAt'>): void {
  sessionStore.setJSON(LAST_ORDER_KEY, { ...order, createdAt: new Date().toISOString() })
}

export function readRememberedOrder(): RememberedOrder | null {
  const raw = sessionStore.getJSON<Partial<RememberedOrder>>(LAST_ORDER_KEY)
  if (!raw || typeof raw.id !== 'number' || typeof raw.trade_no !== 'string') {
    return null
  }
  return {
    id: raw.id,
    trade_no: raw.trade_no,
    productId: typeof raw.productId === 'number' ? raw.productId : 0,
    createdAt: typeof raw.createdAt === 'string' ? raw.createdAt : '',
  }
}

/** 按渠道回跳带的 out_trade_no / trade_no 找回订单 ID。 */
export function findRememberedOrderByTradeNo(tradeNo: string | null): RememberedOrder | null {
  if (!tradeNo) {
    return null
  }
  const remembered = readRememberedOrder()
  return remembered && remembered.trade_no.toLowerCase() === tradeNo.toLowerCase()
    ? remembered
    : null
}
