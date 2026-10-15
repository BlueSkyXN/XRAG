import { ArrowRight } from 'lucide-react'
import { navigate } from '../lib/router'
import { Button } from '../components/button'
import { Empty } from '../components/panel'
import { useSession } from '../lib/session'

/** 空库引导：知识库还没有任何已构建文件时，引导去资料页登记来源并构建，而不是给出"没有命中"。 */
export function EmptyKb({ kb }: { kb: string }) {
  const { may } = useSession()
  const canManage = may('maintainer')
  return (
    <Empty
      title={`知识库 ${kb} 还没有可检索的文件`}
      action={canManage && (
        <Button variant="outline" size="sm" onClick={() => navigate('/sources')}>
          去资料与来源
          <ArrowRight />
        </Button>
      )}
    >
      {canManage ? '登记来源并构建，或上传文件。' : '请联系知识库维护者添加资料。'}
    </Empty>
  )
}
