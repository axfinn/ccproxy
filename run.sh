#!/bin/bash

# CCProxy 运行脚本
# 用法: ./run.sh [选项]
#
# 选项:
#   -p, --port PORT      指定端口 (默认: 8080)
#   -b, --build          先构建再运行
#   -d, --dev            开发模式 (前后端分离运行)
#   -h, --help           显示帮助信息

set -e

# 默认配置
PORT=${CCPROXY_SERVER_PORT:-8080}
BUILD=false
DEV_MODE=false

# 默认环境变量 (如果未设置)
export CCPROXY_JWT_SECRET=${CCPROXY_JWT_SECRET:-"dev-secret-change-in-production"}
export CCPROXY_ADMIN_KEY=${CCPROXY_ADMIN_KEY:-"admin123"}

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -p|--port)
            PORT="$2"
            shift 2
            ;;
        -b|--build)
            BUILD=true
            shift
            ;;
        -d|--dev)
            DEV_MODE=true
            shift
            ;;
        -h|--help)
            echo "CCProxy 运行脚本"
            echo ""
            echo "用法: ./run.sh [选项]"
            echo ""
            echo "选项:"
            echo "  -p, --port PORT      指定端口 (默认: 8080)"
            echo "  -b, --build          先构建再运行"
            echo "  -d, --dev            开发模式 (前后端分离运行)"
            echo "  -h, --help           显示帮助信息"
            echo ""
            echo "环境变量:"
            echo "  CCPROXY_JWT_SECRET   JWT 密钥 (必需)"
            echo "  CCPROXY_ADMIN_KEY    管理员密钥 (必需)"
            echo "  CCPROXY_SERVER_PORT  服务端口"
            echo "  CCPROXY_CLAUDE_API_KEYS  Anthropic API 密钥 (逗号分隔)"
            echo ""
            echo "示例:"
            echo "  ./run.sh                    # 默认运行"
            echo "  ./run.sh -p 8081            # 指定端口 8081"
            echo "  ./run.sh -b -p 8081         # 构建后运行在 8081"
            echo "  ./run.sh -d                 # 开发模式"
            exit 0
            ;;
        *)
            echo "未知选项: $1"
            echo "使用 ./run.sh -h 查看帮助"
            exit 1
            ;;
    esac
done

export CCPROXY_SERVER_PORT=$PORT

# 切换到脚本所在目录
cd "$(dirname "$0")"

if [ "$DEV_MODE" = true ]; then
    echo "=========================================="
    echo "  CCProxy 开发模式"
    echo "=========================================="
    echo ""
    echo "启动后端服务..."

    # 构建后端
    CGO_ENABLED=1 go build -o ccproxy ./cmd/server

    # 在后台启动后端
    ./ccproxy &
    BACKEND_PID=$!

    echo "后端已启动 (PID: $BACKEND_PID)"
    echo "  - API: http://localhost:$PORT"
    echo ""

    # 启动前端开发服务器
    echo "启动前端开发服务器..."
    cd web
    npm run dev &
    FRONTEND_PID=$!

    echo "前端已启动 (PID: $FRONTEND_PID)"
    echo "  - UI: http://localhost:5173/admin/"
    echo ""
    echo "按 Ctrl+C 停止所有服务"

    # 捕获退出信号
    trap "kill $BACKEND_PID $FRONTEND_PID 2>/dev/null; exit" SIGINT SIGTERM

    # 等待
    wait
else
    echo "=========================================="
    echo "  CCProxy"
    echo "=========================================="
    echo ""

    # 构建
    if [ "$BUILD" = true ] || [ ! -f "./ccproxy" ]; then
        echo "正在构建..."

        # 检查前端是否需要构建
        if [ ! -d "./web/dist" ]; then
            echo "  - 构建前端..."
            cd web && npm install && npm run build && cd ..
        fi

        echo "  - 构建后端..."
        CGO_ENABLED=1 go build -o ccproxy ./cmd/server
        echo "构建完成"
        echo ""
    fi

    echo "启动服务..."
    echo "  - 地址: http://localhost:$PORT"
    echo "  - Admin UI: http://localhost:$PORT/admin/"
    echo "  - Admin Key: $CCPROXY_ADMIN_KEY"
    echo ""
    echo "按 Ctrl+C 停止服务"
    echo "=========================================="
    echo ""

    ./ccproxy
fi
