export const jobKinds: Record<string, string> = {
  build: '构建',
  publish: '知识发布',
  wiki: 'Wiki 生成',
  sync: '写回',
  eval: '评测',
  research: '委托调查',
  recipe: '固定配方',
  maintenance: '维护'
}

export const activeStates = ['queued', 'running']
