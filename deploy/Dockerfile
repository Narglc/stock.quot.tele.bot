# gohome bot 镜像（多阶段：静态编译 → distroless）
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/bot main.go

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/bot /app/bot
COPY config/config.yaml /app/config/config.yaml
# 日志写 /app/logs（compose 挂 volume）；MCP 由 MCP_ADDR 控制监听
ENTRYPOINT ["/app/bot", "-f", "/app/config/config.yaml"]
