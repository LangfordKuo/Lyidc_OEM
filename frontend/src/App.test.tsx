import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import App from './App'

function jsonResponse(payload: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => payload,
  } as unknown as Response
}

const healthyEnvelope = {
  code: 0,
  message: 'ok',
  data: { status: 'ok', db: 'up', time: '2026-10-08T14:20:00+08:00' },
}

const degradedEnvelope = {
  code: 500,
  message: '服务器内部错误',
  data: { status: 'degraded', db: 'down', time: '2026-10-08T14:20:00+08:00' },
}

describe('App 冒烟测试', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('渲染脚手架文案与健康状态', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(healthyEnvelope)))

    render(<App />)

    expect(screen.getByText('Lyidc_OEM 脚手架就绪')).toBeInTheDocument()

    await waitFor(() => {
      expect(screen.getByText('up')).toBeInTheDocument()
    })
    expect(screen.getByText('正常')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('数据库不可用时展示降级状态与错误提示', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(degradedEnvelope, 503)))

    render(<App />)

    await waitFor(() => {
      expect(screen.getByText('down')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert')).toHaveTextContent('服务器内部错误')
  })

  it('点击「重新检查」会重新请求接口', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(healthyEnvelope))
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<App />)

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1)
    })

    await user.click(screen.getByRole('button', { name: '重新检查' }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2)
    })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/health', expect.anything())
  })
})
