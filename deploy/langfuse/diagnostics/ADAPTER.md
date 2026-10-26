# 固定镜像调用点适配（候选，未部署）

此适配器为已有离线模块增加三个调用点：通过认证后的 OTLP callback、publisher 的 S3 uploadJson、同 publisher 的队列 add。当前服务镜像、compose 和运行环境不变；候选镜像不会自动运行。旧 README.md 描述模块离线合同；这里描述新增适配。

只支持 Langfuse4.58.0 及 pinned-layout.json 中两个汉化后编译文件的完整 SHA。唯一源码锚点和完整语法检查必须同时通过，版本/哈希/缺失/重复/符号链接不匹配时拒绝修改。不是通用编译文件搜索替换器，也不会自动适配上游更新。

运行时代码固定加载 `/opt/ai-gateway-otel-stages/receiver.cjs`，同进程共享一个模块实例。候选显式默认 `AI_GATEWAY_OTEL_STAGE_DIAGNOSTICS=0`；仅在进程启动时值为字面 `1` 才启用，`true`、请求头、查询参数或模型输入均不能启用。仍是2000ms观察、最多8条日志/8并发/2阶段汇总，不改变任何业务超时、取消或重试。

诊断从通过认证后的 callback 开始，认证、路由匹配、模块加载和更早中间件不在测量范围。other只代表该 callback 内未归为S3/queue的工作；未出现阶段日志不能据此排除更早等待。本地序号连接同一进程的pending/终态，不是客户端trace或供应商请求ID；无新HTTP端点或响应头。

## 候选构建

固定官方基础摘要与现有zh层重新构建，不依赖可变的当前服务标签。构建测试覆盖既有汉化、诊断模块、安装/回退，之后先汉化再严格校验两个编译文件并安装。Dockerfile不包含启用或部署步骤。

```sh
DOCKER_BUILDKIT=0 docker build --network none --pull=false \
  -t ai-gateway-langfuse-zh:4.58.0-otel-stages-v4 \
  -f deploy/langfuse/diagnostics/Dockerfile deploy/langfuse
```

本次BuildKit元数据解析无进展后取消，固定基础摘要已在缓存，改用本机legacy缓存构建，保留弃用提示；没有更换基础摘要或安装依赖。RUN禁网不等于构建器元数据解析禁网。长期构建器兼容性另行处理，不据此放宽固定来源。

根 `.dockerignore` 采用显式源码允许清单；本次另从13个显式文件生成独立构建上下文，未传入任何.env、原始评测档案或凭据。不要把私密配置复制进候选。v1/v2/v3是审查/验收前候选，保留证据但不得作为最终候选；v4以档案中的完整镜像ID为准，不仅依赖标签。

## 离线回退与恢复

安装在web根保留私有manifest及两个原文件备份，原uid/gid通过fchown、原模式通过独占临时文件的fchmod精确保留，避免umask收窄。全部预检查通过后才修改；写入失败回滚已改文件，无法完成时保留恢复材料。

```sh
node /opt/ai-gateway-otel-stages/install.cjs restore /app/web
```

上述命令只用于离线副本/构建验收。它恢复两个源码的原字节和模式，移除自己的web根manifest和备份，不移除镜像复制的/opt模块。未知备份文件、源/备份/模式篡改先拒绝；prepared允许核对原/新混合状态后恢复。源码完全恢复后先持久化restored，再清理备份；清理中断可重新执行，只验证原源码并清理已验证的剩余材料。绝不递归删除未知目录内容。

没有热卸载已经加载模块的承诺；未来运行服务回退应使用已验证原镜像并重建web，仍需独立部署验收。本轮没有执行该操作，也没有删除现有镜像或数据卷。
