FROM public.ecr.aws/docker/library/golang:1.25.12 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY internal ./internal
COPY cmd ./cmd
COPY knowledge ./knowledge
COPY scripts/knowledge-smoke ./scripts/knowledge-smoke
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go run ./cmd/knowledge-manifest -dir knowledge/platform -check && \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /out/agent ./cmd/agent && \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /out/knowledge-smoke ./scripts/knowledge-smoke
FROM scratch AS runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/agent /app/agent
COPY --from=build /src/knowledge/platform /app/knowledge/platform
USER 65532:65532
WORKDIR /app
ENV AGENT_KNOWLEDGE_DIR=/app/knowledge/platform LISTEN_HOST=0.0.0.0
HEALTHCHECK --interval=5s --timeout=3s --start-period=15s --retries=20 CMD ["/app/agent", "-healthcheck"]
ENTRYPOINT ["/app/agent"]
FROM runtime AS smoke
COPY --from=build /out/knowledge-smoke /app/knowledge-smoke
