# 用量统计和使用记录系统

ccproxy 的用量统计、请求日志记录和对话内容记录系统。

## 功能概述

### 1. 详细请求日志记录
- 记录每次 API 请求的完整信息
- 包括请求时间、响应时间、token 使用量、错误信息等
- 支持按 Token ID、Account ID、模式、模型等筛选
- 支持分页和排序
- 支持导出为 CSV 或 JSON 格式

### 2. 对话内容记录（可选）
- 在 Token 级别可控制是否记录对话内容
- 记录用户输入、模型输出、系统提示词、对话历史
- 支持全文搜索
- 自动压缩 7 天前的对话（节省存储空间 60-80%）
- 支持导出为 JSON 或 JSONL 格式

### 3. 统计聚合
- 每日自动聚合统计数据
- Token 级别和 Account 级别的用量统计
- 支持时间范围筛选和趋势分析
- 实时统计和历史统计

### 4. 异步日志处理
- 基于 worker pool 的异步日志记录（4个worker，10000条缓冲）
- 批量写入优化（每批最多100条）
- 不阻塞主请求路径
- 优雅关闭和背压处理

## 数据库表结构

### request_logs 表
```sql
CREATE TABLE request_logs (
    id TEXT PRIMARY KEY,
    token_id TEXT NOT NULL,
    account_id TEXT,
    user_name TEXT,
    mode TEXT NOT NULL,              -- 'web' or 'api'
    model TEXT NOT NULL,
    stream BOOLEAN NOT NULL,
    request_at DATETIME NOT NULL,
    response_at DATETIME,
    duration_ms INTEGER,
    ttft_ms INTEGER,                 -- Time to First Token
    prompt_tokens INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    status_code INTEGER NOT NULL,
    success BOOLEAN NOT NULL,
    error_message TEXT,
    conversation_id TEXT
);
```

### conversation_contents 表
```sql
CREATE TABLE conversation_contents (
    id TEXT PRIMARY KEY,
    request_log_id TEXT NOT NULL,
    token_id TEXT NOT NULL,
    system_prompt TEXT,
    messages_json TEXT NOT NULL,     -- JSON 数组的对话历史
    prompt TEXT NOT NULL,            -- 用户当前输入
    completion TEXT NOT NULL,        -- 模型输出
    created_at DATETIME NOT NULL,
    is_compressed BOOLEAN DEFAULT 0
);
```

### usage_stats_daily 表
```sql
CREATE TABLE usage_stats_daily (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    stat_date DATE NOT NULL,
    token_id TEXT,
    account_id TEXT,
    mode TEXT,
    model TEXT,
    request_count INTEGER DEFAULT 0,
    success_count INTEGER DEFAULT 0,
    error_count INTEGER DEFAULT 0,
    total_prompt_tokens INTEGER DEFAULT 0,
    total_completion_tokens INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    avg_duration_ms INTEGER DEFAULT 0,
    avg_ttft_ms INTEGER DEFAULT 0
);
```

## API 端点

所有端点都需要管理员权限（X-Admin-Key header）。

### 请求日志

#### 列出请求日志
```bash
GET /api/logs/requests

查询参数:
  - token_id: 按 Token ID 筛选
  - account_id: 按 Account ID 筛选
  - user_name: 按用户名筛选
  - mode: 按模式筛选 (web/api)
  - model: 按模型筛选
  - success: 按成功状态筛选 (true/false)
  - from_date: 开始日期 (RFC3339)
  - to_date: 结束日期 (RFC3339)
  - page: 页码 (默认 0)
  - limit: 每页数量 (默认 50)

示例:
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/logs/requests?token_id=xxx&limit=100"
```

#### 获取单个请求日志
```bash
GET /api/logs/requests/:id

示例:
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/logs/requests/log-id-123"
```

#### 导出请求日志
```bash
GET /api/logs/requests/export

查询参数:
  - format: 导出格式 (csv/json，默认 csv)
  - 其他参数同 list 接口

示例 (CSV):
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/logs/requests/export?format=csv&token_id=xxx" \
  -o logs.csv

示例 (JSON):
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/logs/requests/export?format=json" \
  -o logs.json
```

#### 删除旧日志
```bash
DELETE /api/logs/requests/old?days=90

示例:
curl -X DELETE -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/logs/requests/old?days=90"
```

### 对话内容

#### 列出对话
```bash
GET /api/conversations

查询参数:
  - token_id: 按 Token ID 筛选 (必需)
  - from_date: 开始日期 (RFC3339)
  - to_date: 结束日期 (RFC3339)
  - page: 页码 (默认 0)
  - limit: 每页数量 (默认 50)

示例:
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/conversations?token_id=xxx"
```

#### 获取单个对话
```bash
GET /api/conversations/:id

示例:
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/conversations/conv-id-123"
```

#### 全文搜索对话
```bash
GET /api/conversations/search

查询参数:
  - q: 搜索关键词 (必需)
  - token_id: Token ID (必需)
  - limit: 结果数量 (默认 20，最大 100)

示例:
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/conversations/search?q=Claude&token_id=xxx"
```

#### 导出对话
```bash
GET /api/conversations/export

查询参数:
  - format: 导出格式 (json/jsonl，默认 json)
  - 其他参数同 list 接口

示例 (JSONL):
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/conversations/export?format=jsonl&token_id=xxx" \
  -o conversations.jsonl
```

#### 删除对话
```bash
DELETE /api/conversations/:id

示例:
curl -X DELETE -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/conversations/conv-id-123"
```

### 统计数据

#### Token 统计
```bash
GET /api/stats/tokens/:id

查询参数:
  - from_date: 开始日期 (2006-01-02)
  - to_date: 结束日期 (2006-01-02)
  - days: 天数 (代替 from_date/to_date)

响应示例:
{
  "request_count": 1250,
  "success_count": 1200,
  "error_count": 50,
  "total_prompt_tokens": 125000,
  "total_completion_tokens": 75000,
  "total_tokens": 200000,
  "avg_duration_ms": 1500,
  "avg_ttft_ms": 350,
  "success_rate": 96.0
}
```

#### Token 趋势
```bash
GET /api/stats/tokens/:id/trend?days=30

响应示例:
{
  "token_id": "xxx",
  "days": 30,
  "trend": [
    {
      "date": "2026-01-01",
      "request_count": 45,
      "success_count": 43,
      "total_tokens": 8500
    },
    ...
  ]
}
```

#### Account 统计
```bash
GET /api/stats/accounts/:id
GET /api/stats/accounts/:id/trend?days=30

# 参数和响应同 Token 统计
```

#### 全局概览
```bash
GET /api/stats/overview

查询参数:
  - from_date, to_date, days (同上)

响应示例:
{
  "total_tokens": 1500000,
  "total_requests": 5000,
  "total_users": 25,
  "active_tokens": 20,
  "by_mode": {
    "web": { "request_count": 3000, ... },
    "api": { "request_count": 2000, ... }
  },
  "by_model": {
    "claude-opus-4": { "request_count": 2500, ... },
    "claude-sonnet-4": { "request_count": 2500, ... }
  }
}
```

#### 实时统计
```bash
GET /api/stats/realtime

# 返回今天的实时统计（直接从 request_logs 查询）
```

#### Top Tokens
```bash
GET /api/stats/top/tokens?days=7&limit=10

# 返回指定天数内使用量最高的 Tokens
```

#### Top Models
```bash
GET /api/stats/top/models?days=7

# 返回指定天数内使用量最高的模型
```

### Token 设置

#### 更新 Token 设置
```bash
PUT /api/token/:id/settings

请求体:
{
  "enable_conversation_logging": true
}

示例:
curl -X PUT -H "X-Admin-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{"enable_conversation_logging": true}' \
  "http://localhost:8080/api/token/token-id/settings"
```

#### 获取 Token 信息
```bash
GET /api/token/list

# 返回的每个 Token 都包含:
# - enable_conversation_logging: 是否启用对话记录
# - total_requests: 总请求数
# - total_tokens_used: 总 Token 使用量
```

## 后台任务

### 1. 请求日志记录器
- 启动时间: 服务启动
- 运行模式: 持续运行
- Worker 数量: 4
- 缓冲大小: 10000
- 批量大小: 100
- 功能: 异步记录请求日志和对话内容

### 2. 统计聚合器
- 启动时间: 服务启动
- 运行间隔: 24小时
- 功能: 聚合昨天的请求日志数据到 usage_stats_daily 表

### 3. 对话压缩器
- 启动时间: 服务启动
- 运行间隔: 24小时
- 压缩年龄: 7天
- 批量大小: 100
- 功能: 使用 gzip 压缩 7 天前的对话内容，节省存储空间

## 性能优化

### 数据库优化
- WAL 模式启用
- NORMAL 同步模式
- 64MB 缓存
- 批量写入（100条/批）
- 完善的索引策略

### 日志记录优化
- 异步处理，不阻塞主请求
- Worker pool 并发处理
- 缓冲 channel 防止溢出
- 批量数据库写入
- 优雅关闭处理

### 压缩优化
- gzip 压缩（60-80% 存储节省）
- Base64 编码安全存储
- 批量处理
- 透明解压缩（读取时自动）

## 隐私和安全

### 对话记录控制
- 默认禁用对话记录
- 在 Token 级别显式启用
- 只记录启用了对话记录的 Token 的对话

### 数据保留
- 请求日志: 可手动删除旧数据（建议保留 90 天）
- 对话内容: 可手动删除
- 统计数据: 永久保留（数据量小）

### 数据访问
- 所有 API 都需要管理员权限
- 支持按 Token ID 隔离查询
- 敏感数据不在日志中显示

## 使用示例

### 启用 Token 的对话记录
```bash
# 1. 获取 Token 列表
curl -H "X-Admin-Key: your-key" \
  http://localhost:8080/api/token/list

# 2. 启用对话记录
curl -X PUT -H "X-Admin-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{"enable_conversation_logging": true}' \
  "http://localhost:8080/api/token/{token_id}/settings"
```

### 查看 Token 使用情况
```bash
# 查看过去 30 天的统计
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/stats/tokens/{token_id}?days=30"

# 查看趋势图表数据
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/stats/tokens/{token_id}/trend?days=30"
```

### 搜索对话内容
```bash
# 搜索包含特定关键词的对话
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/conversations/search?q=Claude&token_id={token_id}"
```

### 导出数据
```bash
# 导出请求日志为 CSV
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/logs/requests/export?format=csv&token_id={token_id}&from_date=2026-01-01" \
  -o request_logs.csv

# 导出对话为 JSONL
curl -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/conversations/export?format=jsonl&token_id={token_id}" \
  -o conversations.jsonl
```

### 清理旧数据
```bash
# 删除 90 天前的请求日志
curl -X DELETE -H "X-Admin-Key: your-key" \
  "http://localhost:8080/api/logs/requests/old?days=90"
```

## 故障排查

### 日志队列溢出
如果看到日志 "Request log queue full"：
- 增加 buffer_size（默认 10000）
- 增加 workers 数量（默认 4）
- 检查数据库写入性能

### 压缩失败
如果对话压缩失败：
- 检查数据库空间
- 查看日志中的错误信息
- 手动运行压缩（未来可能添加）

### 统计数据不准确
- 检查是否运行了聚合任务
- 手动触发聚合（未来可能添加）
- 检查日志是否正常记录

## 未来改进

- [ ] 前端可视化界面
- [ ] 成本计算（基于 token 使用量）
- [ ] 告警功能（使用量超限、错误率高等）
- [ ] 更多导出格式（Excel、PDF 报表）
- [ ] 自定义统计维度
- [ ] API 限流和配额管理
- [ ] 数据归档到对象存储
