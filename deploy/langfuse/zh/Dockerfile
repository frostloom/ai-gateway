FROM docker.langfuse.com/langfuse/langfuse@sha256:b5f4306480d844b2bce43ecd82960cefc0b219e5164ea588ea45ed806a41a4c9
USER root
COPY translate.cjs translate.test.cjs zh-CN.json /opt/langfuse-zh/
RUN node --test /opt/langfuse-zh/translate.test.cjs && node /opt/langfuse-zh/translate.cjs
USER 1001
