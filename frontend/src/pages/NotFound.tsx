import { Button } from '@heroui/react'
import { useNavigate } from 'react-router-dom'

import { paths } from '../app/paths'
import { EmptyBlock } from '../components/common/PageState'

// 404 页面。
export default function NotFound() {
  const navigate = useNavigate()

  return (
    <div className="mx-auto max-w-3xl px-4 py-20 sm:px-6">
      <EmptyBlock
        title="404 · 页面不存在"
        description="您访问的页面可能已被移除或地址输入有误。"
      />
      <div className="mt-6 flex justify-center gap-3">
        <Button variant="primary" onPress={() => navigate(paths.home)}>
          返回首页
        </Button>
        <Button variant="outline" onPress={() => navigate(paths.products)}>
          浏览商品
        </Button>
      </div>
    </div>
  )
}
