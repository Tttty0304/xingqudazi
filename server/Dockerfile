# 注意：本 Dockerfile 的 build context 是项目根目录（见 deploy/docker-compose.yml），
# 因此下面所有 COPY 的源路径均相对于项目根目录，不是相对于本文件所在的 server/ 目录。

# --- build stage ---
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server

# --- runtime stage ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/server ./server
EXPOSE 8080
ENTRYPOINT ["./server"]
