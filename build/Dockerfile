ARG BUILD_DOCKER_REGISTRY
FROM ${BUILD_DOCKER_REGISTRY}/go-base-image:1.20-art AS builder
ARG BUILD_DATE
ARG COMMIT
ARG VERSION
ADD . /build/
WORKDIR /build
RUN apk add --no-cache git; \
    go env -w GOPRIVATE=scm.x5.ru; \
    export GO111MODULE=on; \
    export CGO_ENABLED=0; \
    go mod tidy -v -compat=1.17; \
    LDFLAGS=" \
    -X 'main.commit=$COMMIT' \
    -X 'main.buildDate=$BUILD_DATE' \
    -X 'main.version=$VERSION'"; \
    go build -v -ldflags "$LDFLAGS" -o ./output/app ./cmd/app

FROM ${BUILD_DOCKER_REGISTRY}/alpine-base-image:0.0.1
WORKDIR /app
COPY --from=builder /build/output/app /app/bin
RUN adduser -D -g "" -h "/app" -G "users" -H -u "1000" "gouser" \
    && chown -R 1000:1000 /app \
    && chmod +x /app/bin
USER 1000
CMD ["/app/bin"]
