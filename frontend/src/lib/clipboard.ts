// 复制到剪贴板：优先 Clipboard API，不可用（http 非安全上下文 / 旧浏览器）时回退到
// 临时 textarea + execCommand，保证「复制」按钮在本地与内网部署下都可用。
export async function copyText(value: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value)
      return true
    }
  } catch {
    // 落到下面的回退实现
  }
  try {
    const area = document.createElement('textarea')
    area.value = value
    area.setAttribute('readonly', 'readonly')
    area.style.position = 'fixed'
    area.style.opacity = '0'
    document.body.appendChild(area)
    area.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(area)
    return ok
  } catch {
    return false
  }
}
