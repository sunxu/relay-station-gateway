# syntax=docker/dockerfile:1.7

FROM golang:1.27.0-alpine

WORKDIR /app

# 安装必要的工具
RUN apk add --no-cache git

# 复制go.mod和go.sum
COPY go.mod go.sum ./

# 下载依赖
RUN --mount=type=cache,id=sub2api-gomod,target=/go/pkg/mod \
    --mount=type=tmpfs,target=/tmp,size=4294967296 \
    go mod download

# 复制源代码
COPY . .

# 构建应用
RUN --mount=type=cache,id=sub2api-gomod,target=/go/pkg/mod \
    --mount=type=cache,id=sub2api-gobuild,target=/root/.cache/go-build \
    --mount=type=tmpfs,target=/tmp,size=4294967296 \
    VERSION_VALUE="$(./scripts/resolve-version.sh)" && \
    go build -ldflags="-s -w -X main.Version=${VERSION_VALUE}" -o main ./cmd/server/

# 暴露端口
EXPOSE 8080

# 运行应用
CMD ["./main"]
