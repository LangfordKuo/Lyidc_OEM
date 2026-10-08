// 剪贴板：navigator.clipboard 在非安全上下文/旧浏览器可能缺失，这里做一层兜底，
// 失败返回 false 由调用方给出提示（不抛错）。

/** 复制文本到剪贴板；成功返回 true。 */
export async function copyText(value: string): Promise<boolean> {
  if (!value) {
    return false
  }
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value)
      return true
    }
  } catch {
    // 继续走 execCommand 兜底。
  }
  try {
    const textarea = document.createElement('textarea')
    textarea.value = value
    textarea.setAttribute('readonly', '')
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(textarea)
    return ok
  } catch {
    return false
  }
}
