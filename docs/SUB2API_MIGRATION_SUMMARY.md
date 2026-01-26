# Sub2API 架构迁移总结

## 核心变更概览

这次重构将 ccproxy 的账户管理和错误处理逻辑从**简单熔断器模式**升级为 **sub2api 的精细化调度模式**，彻底解决了 `unavailable accounts available=0 total=1` 的问题。

---

## 1. Account 数据模型扩展 ✅

### 文件: `internal/store/account.go`

**新增字段 (sub2api style):**

```go
type Account struct {
    // ... 原有字段 ...

    // 🆕 状态管理
    Status       AccountStatus  // active, error, disabled, paused (替代简单的 IsActive)
    Schedulable  bool          // 可调度标志 (核心字段!)
    ErrorMessage string        // 详细错误信息

    // 🆕 时间控制 - 自动恢复机制
    RateLimitedAt        *time.Time  // 限流开始时间
    RateLimitResetAt     *time.Time  // 限流恢复时间 (到期自动恢复)
    OverloadUntil        *time.Time  // 过载保护到期时间
    TempUnschedulableUntil *time.Time  // 临时禁用到期时间 (核心!)
    TempUnschedulableReason string     // 禁用原因

    // 🆕 调度控制
    Priority       int  // 优先级 (数字越小越优先)
    MaxConcurrency int  // 单账户并发限制
}
```

**新增方法:**

```go
// 核心方法 - 5层检查
func (a *Account) IsSchedulable() bool {
    // 1. 基础状态检查
    if a.Status != AccountStatusActive || !a.Schedulable {
        return false
    }

    // 2. 过期检查
    // 3. 过载检查
    // 4. 限流检查 - 到期自动恢复 ✅
    // 5. 临时禁用检查 - 到期自动恢复 ✅
}
```

**对比:**

| 旧逻辑 | 新逻辑 |
|--------|--------|
| ❌ 只有 `is_active` 布尔值 | ✅ `status` + `schedulable` + 时间控制 |
| ❌ 熔断后无法自动恢复 | ✅ 到期时间自动清除 |
| ❌ 无法区分错误类型 | ✅ 429限流/401认证/503过载分别处理 |

---

## 2. 数据库迁移 ✅

### 文件: `internal/store/migration.go` (新建)

**自动添加新列:**

```sql
ALTER TABLE accounts ADD COLUMN status TEXT DEFAULT 'active';
ALTER TABLE accounts ADD COLUMN schedulable INTEGER DEFAULT 1;
ALTER TABLE accounts ADD COLUMN rate_limit_reset_at DATETIME;
ALTER TABLE accounts ADD COLUMN temp_unschedulable_until DATETIME;
-- ... 等10个新字段
```

**智能迁移:**
- ✅ 检测已有列，避免重复添加
- ✅ 自动将旧数据的 `is_active` 迁移到 `status`
- ✅ 启动时自动执行，无需手动干预

---

## 3. 账户选择逻辑重构 ✅

### 旧逻辑 (enhanced_proxy.go)

```go
// ❌ 单一账户选择 → 被熔断器过滤 → 死循环
GetActiveAccount()
→ 返回1个账户
→ IsAvailable(circuit breaker) 返回 false
→ "no available accounts"
```

### 新逻辑 (sub2api_proxy.go - 新建)

```go
// ✅ 候选列表 + 重试 + 排除机制
GetSchedulableAccounts()
→ 返回所有可调度账户列表 (数据库层面已过滤)
→ 按优先级选择最佳账户
→ 失败时加入排除列表
→ 重试选择下一个账户
→ 最多重试3次
```

**关键改进:**
- ✅ **数据库层面过滤**: `WHERE status='active' AND schedulable=1`
- ✅ **双重检查**: SQL过滤后再调用 `IsSchedulable()` 检查时间条件
- ✅ **排除机制**: 失败账户加入黑名单，重试时自动跳过
- ✅ **优先级调度**: `ORDER BY priority ASC, last_used_at ASC`

---

## 4. 错误分类处理器 ✅

### 文件: `internal/handler/error_handler.go` (新建)

**旧逻辑:**
```go
// ❌ 所有失败都记录到熔断器
if err != nil || statusCode != 200 {
    circuit.RecordFailure(accountID)  // 无差别熔断
}
```

**新逻辑:**
```go
switch statusCode {
case 429:  // Rate Limited
    // ✅ 设置限流恢复时间 (60秒后自动恢复)
    resetAt := time.Now().Add(60 * time.Second)
    store.SetAccountRateLimit(accountID, resetAt, "rate_limited")

case 401, 403:  // Auth Error
    // ✅ 标记为错误状态 (需人工修复)
    store.UpdateAccountStatus(accountID, "error", "auth failed")
    store.DeactivateAccount(accountID)

case 503:  // Service Unavailable
    // ✅ 临时过载 (10秒后自动恢复)
    overloadUntil := time.Now().Add(10 * time.Second)
    store.SetAccountOverload(accountID, overloadUntil)

case 500, 502, 504:  // Server Error
    // ✅ 服务器错误，重试但不标记账户失败
    // 使用排除列表切换账户

default:
    // ✅ 成功或客户端错误，不处理
}
```

**自动恢复机制:**
- ✅ 429 限流 → 60秒后 `rate_limit_reset_at` 过期 → `IsSchedulable()` 返回 true
- ✅ 503 过载 → 10秒后 `overload_until` 过期 → 自动恢复
- ✅ 401/403 → 标记 `status=error` → 需要手动刷新 token 后恢复

---

## 5. 新的代理处理器 ✅

### 文件: `internal/handler/sub2api_proxy.go` (新建)

**完整的重试流程:**

```go
func ChatCompletions(c *gin.Context) {
    maxRetries := 3
    var excludedAccounts []string

    for attempt := 0; attempt < maxRetries; attempt++ {
        // 1. 获取可调度账户列表
        accounts := store.GetSchedulableAccounts()

        // 2. 过滤排除列表
        availableAccounts := filter(accounts, excludedAccounts)

        // 3. 选择最佳账户 (priority + LRU)
        account := selectBestAccount(availableAccounts)

        // 4. 执行请求
        resp, err := executeWebRequest(account, req)

        // 5. 错误分类
        if err or statusCode >= 400 {
            shouldSwitch := errorClassifier.ClassifyAndHandleError(resp, account.ID)

            // 6. 决定是否切换账户
            if shouldSwitch && attempt < maxRetries-1 {
                excludedAccounts.append(account.ID)
                continue  // 重试下一个账户
            }
        }

        // 7. 成功 - 记录并返回
        errorClassifier.RecordSuccess(account.ID)
        return response
    }
}
```

**与旧逻辑对比:**

| 旧逻辑 (enhanced_proxy.go) | 新逻辑 (sub2api_proxy.go) |
|----------------------------|---------------------------|
| ❌ 依赖熔断器过滤 | ✅ 数据库层面过滤 |
| ❌ 失败时无法切换账户 | ✅ 自动切换可用账户 |
| ❌ 所有错误都熔断 | ✅ 按错误类型分别处理 |
| ❌ 熔断后需要重启恢复 | ✅ 到期时间自动恢复 |
| ❌ 无优先级调度 | ✅ 支持优先级和LRU |

---

## 6. 路由更新 ✅

### 文件: `cmd/server/main.go`

```go
// 🆕 创建新的处理器
sub2apiProxyHandler := handler.NewSub2APIProxyHandler(db, cfg.Claude.WebURL)

// 🆕 使用新处理器
v1.POST("/chat/completions", sub2apiProxyHandler.ChatCompletions)
```

**向后兼容:**
- ✅ 保留了原有的 `enhancedProxyHandler` 用于 `/v1/messages` 端点
- ✅ 保留了 `webProxyHandler` 和 `apiProxyHandler` 用于特定功能
- ✅ 只替换了主要的 `/v1/chat/completions` 端点

---

## 7. 新增数据库方法 ✅

### 文件: `internal/store/account.go`

```go
// 🆕 获取可调度账户列表
GetSchedulableAccounts() []*Account

// 🆕 设置限流信息
SetAccountRateLimit(id, resetAt, reason)

// 🆕 设置过载保护
SetAccountOverload(id, overloadUntil)

// 🆕 设置临时不可调度
SetAccountTempUnschedulable(id, until, reason)

// 🆕 清除临时标志
ClearAccountTempFlags(id)

// 🆕 更新账户状态
UpdateAccountStatus(id, status, errorMessage)
```

---

## 解决的核心问题

### 问题: `unavailable accounts available=0 total=1`

**根本原因:**
```
OAuth Token 过期
→ 健康检查失败 (403)
→ 熔断器记录失败 × 5
→ 熔断30秒
→ GetActiveAccount() 被熔断器过滤
→ "no available accounts"
→ 30秒后恢复
→ 再次403
→ 再次熔断
→ 死循环 ❌
```

**新逻辑解决方案:**
```
OAuth Token 过期
→ 请求失败 (403)
→ 识别为认证错误
→ 设置 status='error', schedulable=false
→ GetSchedulableAccounts() 数据库层面过滤 (不依赖熔断器!)
→ 如果有其他账户 → 自动切换 ✅
→ 如果没有其他账户 → 返回明确错误信息，等待管理员修复
→ 管理员刷新 token
→ 账户恢复 status='active', schedulable=true
→ 自动可用 ✅
```

---

## 使用指南

### 1. 编译和启动

```bash
# 编译
CGO_ENABLED=1 go build -o ccproxy ./cmd/server

# 启动 (自动执行数据库迁移)
./ccproxy
```

### 2. 测试新逻辑

```bash
# 运行测试脚本
./scripts/test-sub2api.sh

# 或手动测试
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'
```

### 3. 查看账户状态

```bash
# 列出所有账户
curl -H "X-Admin-Key: $ADMIN_KEY" http://localhost:8080/api/account/list | jq '.'

# 查看可调度状态
curl -H "X-Admin-Key: $ADMIN_KEY" http://localhost:8080/api/account/list | \
  jq '.[] | {name, status, schedulable, rate_limit_reset_at, temp_unschedulable_until}'
```

### 4. 手动恢复账户

```bash
# 如果账户被标记为 error，刷新 token
ACCOUNT_ID="acc_xxx"
curl -X POST -H "X-Admin-Key: $ADMIN_KEY" \
  http://localhost:8080/api/account/$ACCOUNT_ID/refresh

# 或通过 SQL 直接清除临时标志
sqlite3 ccproxy.db "UPDATE accounts SET
  schedulable=1,
  status='active',
  rate_limit_reset_at=NULL,
  temp_unschedulable_until=NULL
  WHERE id='$ACCOUNT_ID'"
```

---

## 监控和调试

### 日志关键字

```bash
# 查看账户选择日志
tail -f logs/*.log | grep "selected account"

# 查看账户切换日志
tail -f logs/*.log | grep "switching account"

# 查看限流日志
tail -f logs/*.log | grep "rate limited"

# 查看错误分类日志
tail -f logs/*.log | grep "authentication failed"
```

### 预期日志输出

```
✅ 正常情况:
INF selected account for request account_id=acc_xxx account_name=test attempt=1 available=2

✅ 429 限流自动恢复:
WRN account rate limited, temporarily unscheduling account_id=acc_xxx retry_after_seconds=60
INF switching account due to error account_id=acc_xxx status_code=429
INF selected account for request account_id=acc_yyy (切换到另一个账户)

✅ 401 认证失败:
ERR authentication failed, marking account as error account_id=acc_xxx status_code=401
(账户被标记为 error，需手动修复)

✅ 503 服务不可用:
WRN service unavailable, setting overload protection account_id=acc_xxx
(10秒后自动恢复)
```

---

## 总结

### 改动的文件

**新建:**
1. `internal/store/migration.go` - 数据库迁移
2. `internal/handler/error_handler.go` - 错误分类器
3. `internal/handler/sub2api_proxy.go` - 新代理处理器
4. `scripts/test-sub2api.sh` - 测试脚本
5. `docs/SUB2API_MIGRATION_SUMMARY.md` - 本文档

**修改:**
1. `internal/store/account.go` - 扩展 Account 结构 + 新增方法
2. `internal/store/sqlite.go` - 添加迁移调用
3. `cmd/server/main.go` - 注册新处理器

**未修改 (保留向后兼容):**
- `internal/handler/enhanced_proxy.go` - 保留用于 `/v1/messages`
- `internal/handler/web_proxy.go` - 保留用于 Web 路由
- `internal/handler/api_proxy.go` - 保留用于 API 路由
- `internal/circuit/*` - 熔断器保留但不再依赖

### 核心优势

| 特性 | 旧逻辑 | 新逻辑 (sub2api) |
|------|--------|------------------|
| 账户选择 | ❌ 单一账户 | ✅ 候选列表 + 优先级 |
| 错误恢复 | ❌ 需要重启 | ✅ 自动到期恢复 |
| 限流处理 | ❌ 熔断30秒 | ✅ 60秒后自动恢复 |
| 认证失败 | ❌ 熔断循环 | ✅ 标记error等待修复 |
| 账户切换 | ❌ 不支持 | ✅ 自动重试3次 |
| 过载保护 | ❌ 无 | ✅ 10秒临时禁用 |
| 监控能力 | ❌ 模糊 | ✅ 详细状态和原因 |

**一句话总结:**
从"粗暴熔断"升级为"精细调度 + 自动恢复 + 智能重试"，彻底解决 403 死循环问题！
