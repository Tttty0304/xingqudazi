# 注意：本 Dockerfile 的 build context 是项目根目录（见 deploy/docker-compose.yml），
# 因此下面所有 COPY 的源路径均相对于项目根目录，不是相对于本文件所在的 client/ 目录。

# --- build stage ---
# jsdom 30 / Vitest 4 的锁定依赖要求 Node 22+；与本地测试运行时保持一致。
FROM node:22-alpine AS builder
WORKDIR /src
COPY client/package*.json ./
RUN npm ci
COPY client/ .
# 空字符串表示"同源相对路径"：前端所有 API 调用路径已自带 /api 前缀
# （见 client/src/api/client.ts），此处不能再设为 "/api"，否则请求路径会
# 变成 /api/api/xxx（真机浏览器验证时发现的真实 bug，已修复，见 docker-compose.yml
# 里的同款说明）。
ARG VITE_API_BASE_URL=
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
RUN npm run build

# --- runtime stage ---
FROM nginx:1.27-alpine
COPY --from=builder /src/dist /usr/share/nginx/html
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
