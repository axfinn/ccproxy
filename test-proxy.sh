#!/bin/bash

# ccproxy 测试脚本
# 用法: ./test-proxy.sh [TOKEN] [BASE_URL]

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 默认值
BASE_URL="${2:-http://localhost:8080}"
TOKEN="${1}"

echo -e "${BLUE}=== ccproxy 测试脚本 ===${NC}\n"

# 检查 TOKEN
if [ -z "$TOKEN" ]; then
    echo -e "${RED}错误: 请提供访问令牌${NC}"
    echo "用法: $0 <TOKEN> [BASE_URL]"
    echo "示例: $0 your_token_here http://localhost:8080"
    exit 1
fi

echo -e "${YELLOW}配置信息:${NC}"
echo "  BASE_URL: $BASE_URL"
echo "  TOKEN: ${TOKEN:0:20}..."
echo ""

# 测试函数
test_endpoint() {
    local name=$1
    local method=$2
    local endpoint=$3
    local data=$4

    echo -e "${BLUE}测试: $name${NC}"

    if [ "$method" = "GET" ]; then
        response=$(curl -s -w "\n%{http_code}" \
            -H "Authorization: Bearer $TOKEN" \
            "$BASE_URL$endpoint")
    else
        response=$(curl -s -w "\n%{http_code}" \
            -X "$method" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d "$data" \
            "$BASE_URL$endpoint")
    fi

    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')

    if [ "$http_code" -ge 200 ] && [ "$http_code" -lt 300 ]; then
        echo -e "${GREEN}✓ 成功 (HTTP $http_code)${NC}"
        if [ -n "$body" ] && [ "$body" != "null" ]; then
            echo "$body" | jq -C '.' 2>/dev/null || echo "$body"
        fi
        echo ""
        return 0
    else
        echo -e "${RED}✗ 失败 (HTTP $http_code)${NC}"
        echo "$body" | jq -C '.' 2>/dev/null || echo "$body"
        echo ""
        return 1
    fi
}

# 开始测试
echo -e "${YELLOW}=== 开始测试 ===${NC}\n"

# 1. 测试模型列表
test_endpoint "获取模型列表" "GET" "/v1/models" ""

# 2. 测试非流式聊天
chat_data='{
  "model": "claude-3-5-sonnet-20241022",
  "messages": [
    {
      "role": "user",
      "content": "Hello! Please respond with exactly: Test successful"
    }
  ],
  "max_tokens": 100,
  "stream": false
}'

test_endpoint "非流式聊天完成" "POST" "/v1/chat/completions" "$chat_data"

# 3. 测试流式聊天
echo -e "${BLUE}测试: 流式聊天完成${NC}"
stream_data='{
  "model": "claude-3-5-sonnet-20241022",
  "messages": [
    {
      "role": "user",
      "content": "Count from 1 to 3"
    }
  ],
  "max_tokens": 50,
  "stream": true
}'

stream_response=$(curl -s \
    -X POST \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "$stream_data" \
    "$BASE_URL/v1/chat/completions")

if echo "$stream_response" | grep -q "data:"; then
    echo -e "${GREEN}✓ 流式响应成功${NC}"
    echo "前3行响应:"
    echo "$stream_response" | head -3
    echo "..."
    echo ""
else
    echo -e "${RED}✗ 流式响应失败${NC}"
    echo "$stream_response"
    echo ""
fi

# 4. 测试 Web 模式（如果令牌支持）
echo -e "${BLUE}测试: Web 模式 (可选)${NC}"
web_response=$(curl -s -w "\n%{http_code}" \
    -X POST \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -H "X-Proxy-Mode: web" \
    -d "$chat_data" \
    "$BASE_URL/v1/chat/completions")

web_code=$(echo "$web_response" | tail -n1)
if [ "$web_code" -eq 200 ]; then
    echo -e "${GREEN}✓ Web 模式可用${NC}"
elif [ "$web_code" -eq 503 ]; then
    echo -e "${YELLOW}⚠ Web 模式不可用 (未配置 Session)${NC}"
else
    echo -e "${YELLOW}⚠ Web 模式测试失败 (HTTP $web_code)${NC}"
fi
echo ""

# 5. 测试 API 模式（如果令牌支持）
echo -e "${BLUE}测试: API 模式 (可选)${NC}"
api_response=$(curl -s -w "\n%{http_code}" \
    -X POST \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -H "X-Proxy-Mode: api" \
    -d "$chat_data" \
    "$BASE_URL/v1/chat/completions")

api_code=$(echo "$api_response" | tail -n1)
if [ "$api_code" -eq 200 ]; then
    echo -e "${GREEN}✓ API 模式可用${NC}"
elif [ "$api_code" -eq 503 ]; then
    echo -e "${YELLOW}⚠ API 模式不可用 (未配置 API Key)${NC}"
else
    echo -e "${YELLOW}⚠ API 模式测试失败 (HTTP $api_code)${NC}"
fi
echo ""

# 测试完成
echo -e "${GREEN}=== 测试完成 ===${NC}\n"

# Claude Code 配置示例
echo -e "${YELLOW}=== Claude Code 配置示例 ===${NC}\n"

echo "1. 环境变量配置 (推荐):"
echo -e "${BLUE}"
cat <<EOF
export ANTHROPIC_BASE_URL="$BASE_URL/v1"
export ANTHROPIC_API_KEY="$TOKEN"
EOF
echo -e "${NC}"

echo "2. 配置文件 (~/.config/claude/config.json):"
echo -e "${BLUE}"
cat <<EOF
{
  "api": {
    "baseUrl": "$BASE_URL/v1",
    "key": "$TOKEN"
  }
}
EOF
echo -e "${NC}"

echo "3. 测试 Claude Code:"
echo -e "${BLUE}"
echo "claude \"Hello, test connection\""
echo -e "${NC}"

echo -e "\n${GREEN}提示: 将上述环境变量添加到 ~/.bashrc 或 ~/.zshrc 中${NC}"
