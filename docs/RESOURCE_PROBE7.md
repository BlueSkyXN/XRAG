> **历史资源实验**：数值对应当时版本、合成样本与机器。当前测量方式见 [开发指南](DEVELOPMENT.md)，验证范围见 [STATUS.md](STATUS.md)。原 checks 路径是执行时产物，不随源码分发。

# Demo.7 语义/组合查询资源实验

同一个临时SQLite、600份合成文档、每份12块，共7200块，每块128维手工固定非零向量。独立受控HTTP仅返回固定查询向量。每个动作每个路径运行3次，交替运行并在每次前做Go GC；没有Qdrant/Neo4j或真实Embedding服务。

比较当前程序的旧完整View兼容执行路径与新的metadata projection路径，两者的前3项ID及顺序在每次实验中相同。测试临时库，不使用业务文档。

| 动作 | 路径 | 耗时中位数ms | Go累计分配中位数MiB |
|---|---|---:|---:|
| search.semantic | full_view | 258.59 | 114.34 |
| search.semantic | compact_projection | 263.31 | 64.71 |
| +ask | full_view | 262.25 | 115.47 |
| +ask | compact_projection | 273.14 | 66.42 |

测量来自 runtime.MemStats.TotalAlloc 的差值，不是RSS/峰值堆；墙钟包含本机fixture HTTP与缓存结果。数值支持“本样本减少Go分配”，不支持“所有检索快几十倍”或生产吞吐承诺。逐向量精确扫描仍做O(N)CPU工作，耗时变化有限。元数据仍O(N)，并非恒定内存。

```bash
XRAG_COMPACT_REPORT=/tmp/resource-probe7.json go test ./tests -run '^TestDemo7ResourceProbe$' -count=1 -v
```

普通测试默认跳过这项。原始结果：`checks/demo7/resource-probe.json`；执行日志：`resource-probe.log`。
