FROM docker.langfuse.com/langfuse/langfuse@sha256:b5f4306480d844b2bce43ecd82960cefc0b219e5164ea588ea45ed806a41a4c9
USER root
COPY zh/translate.cjs zh/translate.test.cjs zh/assets.cjs zh/assets.test.cjs zh/zh-CN.json /opt/langfuse-zh/
COPY diagnostics/otel-stages.cjs diagnostics/otel-stages.test.cjs diagnostics/receiver.cjs diagnostics/install.cjs diagnostics/install.test.cjs diagnostics/pinned-layout.json /opt/ai-gateway-otel-stages/
RUN node --test /opt/langfuse-zh/translate.test.cjs /opt/langfuse-zh/assets.test.cjs /opt/ai-gateway-otel-stages/otel-stages.test.cjs /opt/ai-gateway-otel-stages/install.test.cjs
RUN node /opt/langfuse-zh/translate.cjs && node /opt/ai-gateway-otel-stages/install.cjs install /app/web
ENV AI_GATEWAY_OTEL_STAGE_DIAGNOSTICS=0
USER 1001
