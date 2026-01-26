# ccproxy vs sub2api 架构对比分析

## 1. Account 数据结构对比

### ccproxy (简单)
```go
type Account struct {
    ID             string
    Name           string
    Type           AccountType  // oauth, session_key, api_key
    Credentials    Credentials
    OrganizationID string
    ExpiresAt      *time.Time
    CreatedAt      time.Time
    LastUsedAt     *time.Time
    IsActive       bool         // 只有这一个状态标志 ❌
    // Health check
    LastCheckAt   *time.Time
    HealthStatus  string       // "healthy", "unhealthy", "unknown"
    ErrorCount    int
    SuccessCount  int
}
```

**问题:**
- ❌ **没有 `Schedulable` 字段** - 无法区分"账户存在"和"可用于调度"
- ❌ **没有临时不可调度机制** - 429限流后无法自动恢复
- ❌ **没有 `Status` 字段** - 无法区分 active/error/disabled 状态
- ❌ **没有并发控制字段** - 无法限制单账户并发

### sub2api (完善)
```go
type Account struct {
    ID          int64
    Name        string
    Platform    string       // anthropic, gemini, openai, antigravity
    Type        string       // oauth, session_key, api_key, setup_token
    Credentials map[string]any
    Concurrency int          // 账户级并发限制 ✅
    Priority    int          // 优先级(数字越小越优先) ✅

    // 核心状态管理
    Status             string  // active, error, disabled, paused ✅
    ErrorMessage       string
    Schedulable        bool    // 可调度标志 ✅

    // 多维度时间控制
    LastUsedAt         *time.Time
    ExpiresAt          *time.Time
    AutoPauseOnExpired bool

    // 限流管理 ✅
    RateLimitedAt      *time.Time
    RateLimitResetAt   *time.Time  // 限流自动解除时间

    // 过载管理 ✅
    OverloadUntil      *time.Time  // 过载保护时间

    // 临时不可调度 ✅
    TempUnschedulableUntil  *time.Time  // 临时禁用到期时间
    TempUnschedulableReason string

    // 会话窗口 ✅
    SessionWindowStart  *time.Time
    SessionWindowEnd    *time.Time
    SessionWindowStatus string

    // 关联关系
    ProxyID       *int64
    AccountGroups []AccountGroup
    GroupIDs      []int64
}
```

**优势:**
- ✅ **细粒度状态控制** - Status(active/error/disabled) + Schedulable(true/false)
- ✅ **临时不可调度** - 429限流后自动设置 TempUnschedulableUntil，到期自动恢复
- ✅ **过载保护** - OverloadUntil 避免高负载账户被过度使用
- ✅ **会话窗口** - 限制账户在特定时间段可用
- ✅ **优先级调度** - Priority 字段支持账户分级

---

## 2. IsSchedulable() 实现对比

### ccproxy (无此方法)
```go
// GetActiveAccount 只检查 is_active 和 expires_at
WHERE is_active = 1 AND (expires_at IS NULL OR expires_at > datetime('now'))
ORDER BY last_used_at DESC, created_at DESC
LIMIT 1
```

**问题:**
- ❌ 获取账户后被熔断器过滤 → 死循环
- ❌ 无法区分"账户有效"和"当前可调度"
- ❌ 限流后无法自动恢复

### sub2api (完善)
```go
func (a *Account) IsSchedulable() bool {
    // 1. 基础状态检查
    if !a.IsActive() || !a.Schedulable {
        return false
    }

    now := time.Now()

    // 2. 过期检查
    if a.AutoPauseOnExpired && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt) {
        return false
    }

    // 3. 过载检查 - 过载期间不调度 ✅
    if a.OverloadUntil != nil && now.Before(*a.OverloadUntil) {
        return false
    }

    // 4. 限流检查 - 限流期间不调度,到期自动恢复 ✅
    if a.RateLimitResetAt != nil && now.Before(*a.RateLimitResetAt) {
        return false
    }

    // 5. 临时不可调度 - 到期自动恢复 ✅
    if a.TempUnschedulableUntil != nil && now.Before(*a.TempUnschedulableUntil) {
        return false
    }

    return true
}
```

**优势:**
- ✅ **自动恢复** - 限流/过载/临时禁用都有到期时间
- ✅ **精确控制** - 5层检查确保账户真正可用
- ✅ **避免死循环** - 不依赖熔断器,数据库层面就过滤

---

## 3. 账户选择策略对比

### ccproxy (单一策略)
```go
// 直接从数据库获取单个账户
GetActiveAccount() -> 返回一个账户 -> 被熔断器过滤 -> 无账户可用
```

**流程:**
1. SQL: `WHERE is_active=1 LIMIT 1`
2. 熔断器检查: `IsAvailable(accountID)` → false
3. 报错: "no active account available"

**问题:**
- ❌ 没有候选列表,被熔断后无fallback
- ❌ 没有排除机制,重试时还是选同一个账户
- ❌ 没有粘性会话管理

### sub2api (多层选择)
```go
SelectAccountWithLoadAwareness(ctx, groupID, sessionHash, model, excludedIDs) {
    // Layer 1: 粘性会话 (Sticky Session) ✅
    if sessionHash != "" {
        accountID := cache.GetSessionAccountID(sessionHash)
        if accountID > 0 && !excluded[accountID] {
            account := getSchedulableAccount(accountID)
            if account.IsSchedulable() {
                // 尝试获取并发槽
                if tryAcquireSlot(account) {
                    return account
                }
                // 并发满,但可等待
                if waitingCount < maxWaiting {
                    return WaitPlan{account, timeout}
                }
            } else {
                // 账户不可用,清理粘性会话 ✅
                cache.DeleteSessionAccountID(sessionHash)
            }
        }
    }

    // Layer 2: 列出所有可调度账户
    accounts := listSchedulableAccounts(groupID, platform)
    // 数据库查询已经过滤:
    // - status = 'active'
    // - schedulable = true
    // - expires_at > now (if AutoPauseOnExpired)

    // Layer 3: 过滤排除列表
    candidates := filter(accounts, func(acc) {
        return !excluded[acc.ID] &&
               acc.IsSchedulable() &&      // 再次检查限流/过载
               acc.SupportsModel(model)
    })

    // Layer 4: 负载感知选择 ✅
    for _, acc := range candidates {
        if tryAcquireSlot(acc) {
            bindStickySession(sessionHash, acc.ID)
            return acc
        }
    }

    // Layer 5: 等待计划 ✅
    bestAccount := selectByPriority(candidates)
    if waitingCount < maxWaiting {
        return WaitPlan{bestAccount, timeout}
    }

    return error("no available accounts")
}
```

**优势:**
- ✅ **粘性会话** - 同一会话绑定同一账户(缓存优化)
- ✅ **自动清理** - 账户不可用时清理粘性绑定
- ✅ **排除机制** - 重试时自动排除失败账户
- ✅ **负载感知** - 检查并发槽,满了可等待
- ✅ **优先级调度** - 多账户时按priority排序

---

## 4. 错误处理对比

### ccproxy (粗暴熔断)
```go
// 所有失败都记录到熔断器
func recordAccountError(accountID string) {
    circuit.RecordFailure(accountID)  // ❌ 无差别熔断
    store.IncrementAccountError(accountID)
}

// 配置
FailureThreshold: 5      // 5次失败 → 熔断
OpenTimeout: 30s         // 熔断30秒
```

**问题:**
- ❌ **无差别熔断** - 429限流、503服务故障、401认证失败都触发熔断
- ❌ **无自动恢复** - 熔断器状态在内存,重启服务才能清除
- ❌ **限流死循环** - 429限流 → 熔断 → 30秒后重试 → 仍然限流 → 再次熔断

### sub2api (细粒度处理)
```go
// 错误分类处理
func handleUpstreamError(resp *http.Response, account *Account) {
    switch resp.StatusCode {
    case 429:  // Rate Limited
        // 设置限流恢复时间 ✅
        resetTime := parseRetryAfter(resp.Header)
        account.RateLimitResetAt = &resetTime
        account.TempUnschedulableUntil = &resetTime
        account.TempUnschedulableReason = "rate_limited"
        // 60秒后自动恢复,不是永久熔断 ✅

    case 401, 403:  // Auth Error
        // 标记账户错误,需要人工处理 ✅
        account.Status = "error"
        account.ErrorMessage = "authentication failed"
        account.Schedulable = false

    case 503:  // Service Unavailable
        // 临时过载,10秒后恢复 ✅
        overloadUntil := time.Now().Add(10 * time.Second)
        account.OverloadUntil = &overloadUntil

    case 500, 502, 504:  // Server Error
        // 服务器错误,重试但不标记账户失败 ✅
        // 使用排除列表切换账户,不影响账户状态

    default:
        // 成功或客户端错误,不处理
    }
}
```

**优势:**
- ✅ **自动恢复** - 限流/过载有明确到期时间
- ✅ **永久失败** - 401/403设置status=error,需人工修复
- ✅ **临时失败** - 503设置过载时间,自动恢复
- ✅ **不影响重试** - 5xx错误切换账户但不标记失败

---

## 5. 健康检查对比

### ccproxy (被动检查)
```go
// 定期健康检查
func (m *monitor) checkAccount(account *Account) {
    resp := sendTestRequest(account)
    if resp.StatusCode != 200 {
        circuit.RecordFailure(account.ID)  // ❌ 触发熔断
        store.UpdateAccountHealth(account.ID, "unhealthy")
    } else {
        circuit.RecordSuccess(account.ID)
        store.UpdateAccountHealth(account.ID, "healthy")
    }
}
```

**问题:**
- ❌ 健康检查失败 → 触发熔断 → 账户不可用
- ❌ 没有自动刷新token机制
- ❌ 检查间隔5分钟,响应慢

### sub2api (主动管理)
```go
// 健康检查 + 主动恢复
func (s *AccountService) healthCheckAndRecover(account *Account) {
    // 1. 检查是否需要刷新token ✅
    if account.IsOAuth() && account.NeedsRefresh() {
        s.refreshOAuthToken(account)
    }

    // 2. 检查临时不可调度是否到期 ✅
    if account.TempUnschedulableUntil != nil {
        if time.Now().After(*account.TempUnschedulableUntil) {
            account.TempUnschedulableUntil = nil
            account.TempUnschedulableReason = ""
            account.Schedulable = true  // 自动恢复 ✅
        }
    }

    // 3. 检查限流是否到期 ✅
    if account.RateLimitResetAt != nil {
        if time.Now().After(*account.RateLimitResetAt) {
            account.RateLimitResetAt = nil
            account.RateLimitedAt = nil
            account.Schedulable = true  // 自动恢复 ✅
        }
    }

    // 4. 执行健康检查
    resp := sendTestRequest(account)
    if resp.StatusCode == 200 {
        if account.Status == "error" {
            account.Status = "active"  // 错误账户恢复 ✅
        }
    }
}
```

**优势:**
- ✅ **主动恢复** - 到期时间自动清除,不依赖检查
- ✅ **Token刷新** - OAuth自动刷新,避免过期
- ✅ **状态修复** - 错误账户测试成功后自动恢复

---

## 6. 并发控制对比

### ccproxy (无)
```go
// ❌ 没有账户级并发控制
// 所有请求都发往同一账户,容易触发限流
```

### sub2api (完善)
```go
// 账户并发槽管理
type ConcurrencyService struct {
    accountSlots map[int64]*Semaphore  // 每账户独立信号量 ✅
}

func (s *ConcurrencyService) AcquireAccountSlot(accountID, concurrency) {
    sem := s.getOrCreateSemaphore(accountID, concurrency)

    // 尝试获取槽
    if sem.TryAcquire() {
        return Acquired{true}
    }

    // 槽满,检查等待队列
    waitingCount := sem.WaitingCount()
    if waitingCount >= maxWaiting {
        return error("wait queue full")
    }

    // 可等待,返回等待计划
    return WaitPlan{
        AccountID: accountID,
        Timeout:   30 * time.Second,
    }
}
```

**优势:**
- ✅ **单账户限流** - 避免同时并发过多请求
- ✅ **等待队列** - 槽满时排队而不是直接失败
- ✅ **公平调度** - 多账户时轮流使用

---

## 核心差异总结

| 特性 | ccproxy | sub2api |
|------|---------|---------|
| **账户状态管理** | IsActive(bool) | Status + Schedulable + 多维时间控制 |
| **临时不可调度** | ❌ 无 | ✅ TempUnschedulableUntil 自动恢复 |
| **限流处理** | ❌ 熔断30秒 | ✅ RateLimitResetAt 自动恢复 |
| **过载保护** | ❌ 无 | ✅ OverloadUntil 临时禁用 |
| **错误分类** | ❌ 所有失败都熔断 | ✅ 429/401/503分别处理 |
| **粘性会话** | ❌ 无 | ✅ Session Hash 绑定账户 |
| **并发控制** | ❌ 无 | ✅ 账户级信号量 + 等待队列 |
| **优先级调度** | ❌ 无 | ✅ Priority 字段 |
| **账户选择** | ❌ 单一账户 | ✅ 候选列表 + 负载感知 |
| **Token刷新** | ✅ 有但被动 | ✅ 主动检查 + 自动刷新 |
| **恢复机制** | ❌ 重启服务 | ✅ 到期时间自动清除 |

---

## 你的 403 问题为何发生

### ccproxy 的死循环

```
1. OAuth Token 过期
2. 健康检查失败(403) → RecordFailure
3. 连续5次失败 → 熔断器打开
4. GetActiveAccount() 获取账户 → 被熔断器过滤
5. 报错: "unavailable accounts available=0 total=1"
6. 30秒后熔断器进入half-open
7. 再次健康检查 → 仍然403(token还是过期) → 再次熔断
8. 回到步骤4,死循环 ❌
```

### sub2api 的处理

```
1. OAuth Token 过期
2. 请求失败(403)
3. 识别为认证错误 → 设置:
   - Status = "error"
   - Schedulable = false
   - ErrorMessage = "authentication failed: 403"
4. IsSchedulable() 返回 false
5. listSchedulableAccounts() 数据库层面过滤
6. 选择其他可用账户 ✅
7. 管理员修复账户(刷新token或重新登录)
8. 健康检查成功 → Status = "active", Schedulable = true
9. 账户恢复可用 ✅
```

---

## 建议改进方案

### 短期修复(立即可用)

```bash
# 1. 禁用熔断器
export CCPROXY_CIRCUIT_ENABLED=false

# 2. 手动刷新token
curl -X POST -H "X-Admin-Key: $KEY" \
  http://localhost:8080/api/account/{account_id}/refresh

# 3. 或者提高熔断阈值
export CCPROXY_CIRCUIT_FAILURE_THRESHOLD=100
export CCPROXY_CIRCUIT_OPEN_TIMEOUT=5s
```

### 中期改进(参考sub2api)

1. **添加 Schedulable 字段**
   ```sql
   ALTER TABLE accounts ADD COLUMN schedulable BOOLEAN DEFAULT TRUE;
   ALTER TABLE accounts ADD COLUMN temp_unschedulable_until DATETIME;
   ALTER TABLE accounts ADD COLUMN rate_limit_reset_at DATETIME;
   ```

2. **修改错误处理**
   ```go
   func recordAccountError(accountID string, statusCode int) {
       switch statusCode {
       case 429:
           // 限流,设置60秒后恢复
           resetAt := time.Now().Add(60 * time.Second)
           store.UpdateAccountTempUnschedulable(accountID, resetAt, "rate_limited")
       case 401, 403:
           // 认证失败,标记为error
           store.UpdateAccountStatus(accountID, "error", "auth failed")
           store.DeactivateAccount(accountID)
       default:
           // 其他错误不影响账户状态
       }
   }
   ```

3. **改造账户选择**
   ```go
   func GetSchedulableAccounts() []*Account {
       // 返回候选列表,不是单个账户
       accounts := store.ListAccounts()
       var schedulable []*Account
       for _, acc := range accounts {
           if acc.IsSchedulable() {  // 检查多维度条件
               schedulable = append(schedulable, acc)
           }
       }
       return schedulable
   }
   ```

### 长期优化(完整重构)

完全参考 sub2api 架构:
- Account 数据模型重构
- 粘性会话管理
- 并发控制系统
- 负载感知调度
- 优先级队列
