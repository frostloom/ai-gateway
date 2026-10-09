# models：100条线下标注草案

回答占位符需要后续fixture真值填充，非真实执行结果。所有条目尚非独立审核金标。

| ID | 原话 | 正确下一步（草案） | 允许工具 | 期望回答/事实模板 | 缺参或备注 |
|---|---|---|---|---|---|
| models-038 | 价格表 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-040 | 模型价格 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-039 | 全部模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-087 | 麻烦您帮我查一下模型目录 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-032 | 查模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-098 | 给我查模型列表 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-003 | 查看模型目录 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-069 | 你们有什么大模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-090 | 把模型都列出来 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-018 | 你们家都有啥 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-034 | 商品列表 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-089 | 模型列表给我 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-047 | 有那些模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-046 | 帮我差模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-022 | 都有啥模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-004 | 我要看商品列表 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-041 | 查模形 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-042 | 模行列表 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-083 | 劳驾帮我列下可用模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-026 | 有啥推荐的模型 | 查询并说明能力边界 | list_models | {actual_model_prices}；{capability_unknown_or_workload_needed}。 | 目录价格不能证明模型能力或性价比；所需能力字段缺失需明说，不臆造。 |
| models-011 | 查询支持的模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-097 | 模型价格亮出来 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-021 | 你们平台有什么 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-074 | list 一下所有模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-078 | 查下 glm 的价格 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-085 | 方便的话帮我列一下所有模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-054 | 我需要在几个模型之间做选择，麻烦给我一份完整的模型目录和价格表 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-019 | 都有哪些能买的 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-075 | 帮我查 model 目录 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-059 | 麻烦给我一份模型目录，我要看看有没有支持长上下文的 | 查询并说明能力边界 | list_models | {actual_model_prices}；{capability_unknown_or_workload_needed}。 | 目录价格不能证明模型能力或性价比；所需能力字段缺失需明说，不臆造。 |
| models-056 | 我想选一个性价比高的模型，能给我看看所有可选模型和价格吗 | 查询并说明能力边界 | list_models | {actual_model_prices}；{capability_unknown_or_workload_needed}。 | 目录价格不能证明模型能力或性价比；所需能力字段缺失需明说，不臆造。 |
| models-079 | 请问你们有哪些模型可以选 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-091 | 立刻查模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-064 | 支持长文本的有哪些 | 查询并说明能力边界 | list_models | {actual_model_prices}；{capability_unknown_or_workload_needed}。 | 目录价格不能证明模型能力或性价比；所需能力字段缺失需明说，不臆造。 |
| models-077 | 有没有 claude 模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-031 | 模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-014 | 查商品目录 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-013 | 模型目录查询 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-035 | 看模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-088 | 请问模型都有哪些选择 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-024 | 你们这都有什么货 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-037 | 模型目录 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-048 | 模型价各 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-005 | 模型价格查询 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-058 | 我想知道你们接入了哪些厂商的模型，以及大概的价格区间 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-033 | 模型列表 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-099 | 模型清单发我 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-007 | 列出所有模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-061 | 最便宜的模型是哪个 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-063 | 哪个模型最划算 | 查询并说明能力边界 | list_models | {actual_model_prices}；{capability_unknown_or_workload_needed}。 | 目录价格不能证明模型能力或性价比；所需能力字段缺失需明说，不臆造。 |
| models-012 | 看看有哪些模型可买 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-052 | 你好，我准备接入你们的服务，想先看看你们提供哪些模型以及各自的收费标准 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-096 | 直接列模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-076 | 看下各个 model 的 price | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-045 | 模型列别 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-071 | 查下 model list | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-068 | 性价比最高的是哪个 | 查询并说明能力边界 | list_models | {actual_model_prices}；{capability_unknown_or_workload_needed}。 | 目录价格不能证明模型能力或性价比；所需能力字段缺失需明说，不臆造。 |
| models-084 | 请问有哪些模型是免费的呢 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-067 | 哪个模型上下文最长 | 查询并说明能力边界 | list_models | {actual_model_prices}；{capability_unknown_or_workload_needed}。 | 目录价格不能证明模型能力或性价比；所需能力字段缺失需明说，不臆造。 |
| models-050 | 查下默写 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-006 | 帮我查一下可用的模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-073 | 查下 gpt 系列的 price | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-070 | 哪个厂商的便宜 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-001 | 查询模型列表 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-095 | 模型目录，快点 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-065 | 你们接入了 DeepSeek 吗 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-027 | 我想看看你们的商品 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-049 | 模形价格 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-036 | 有哪些模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-028 | 都有些什么可以选择 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-010 | 查下模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-025 | 能用的模型有哪些 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-029 | 都有哪些便宜又好用的 | 查询并说明能力边界 | list_models | {actual_model_prices}；{capability_unknown_or_workload_needed}。 | 目录价格不能证明模型能力或性价比；所需能力字段缺失需明说，不臆造。 |
| models-015 | 你们卖啥模型啊 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-082 | 可以帮忙查询一下模型价格吗 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-044 | 商品列标 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-030 | 看看有什么新模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-043 | 查下魔性 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-009 | 我要看模型清单 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-100 | 快速列下模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-055 | 请问你们的模型商城里都有哪些商品？我想比较一下不同厂商的价格 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-017 | 我想看看有啥好用的 | 查询并说明能力边界 | list_models | {actual_model_prices}；{capability_unknown_or_workload_needed}。 | 目录价格不能证明模型能力或性价比；所需能力字段缺失需明说，不臆造。 |
| models-062 | 有没有免费的模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-057 | 帮我把目前上架的模型都列出来，我要评估一下成本 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-053 | 帮我列一下现在能用的模型清单吧，包括它们的输入输出价格 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-093 | 模型报价给我 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-081 | 能不能给我看看模型目录 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-080 | 麻烦帮我列一下模型清单好吗 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-072 | 你们支持哪些 model | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-002 | 你们有哪些模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-086 | 能否提供一份模型价格表 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-020 | 给我看看货 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-060 | 我打算批量采购，先要一份完整的模型和价格清单 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-023 | 我想买个模型，先看看有啥 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-008 | 查看模型价格 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-051 | 我想了解一下你们平台上目前支持调用的模型都有哪些，最好能带上价格 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-092 | 马上列模型清单 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-094 | 报一下所有模型 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-016 | 都有什么模型能用 | 查询并回答 | list_models | {requested_model_catalog_and_prices}。 |  |
| models-066 | 有没有视觉模型 | 查询并说明能力边界 | list_models | {actual_model_prices}；{capability_unknown_or_workload_needed}。 | 目录价格不能证明模型能力或性价比；所需能力字段缺失需明说，不臆造。 |
