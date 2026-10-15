import { ArrowRight } from 'lucide-react'
import { navigate } from '../lib/router'
import { Button } from '../components/button'
import { Empty } from '../components/panel'

/** 空库引导：知识库还没有任何已构建文件时，引导去资料页登记来源并构建，而不是给出"没有命中"。 */
export function EmptyKb({ kb }: { kb: string }) {
  return (
    <Empty
      title={`知识库 ${kb} 还没有可检索的文件`}
      action={
        <Button variant="outline" size="sm" onClick={() => navigate('/sources')}>
          去资料与来源
          <ArrowRight />
        </Button>
      }
    >
      登记来源或上传文件后运行构建，构建成功的文件才会进入检索。
    </Empty>
  )
}
