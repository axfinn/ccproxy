# 账户健康检查详解

## 健康检查触发时机

1. **自动后台检查**: 每 5 分钟执行一次 (`config.CheckInterval`)
2. **手动检查**: 调用 `/api/account/:id/check` 端点
3. **启动时检查**: Health Monitor 启动后立即执行

---

## 账户被标记不健康的情况

### 🔴 OAuth 账户 (type=oauth)

#### 检查逻辑 (`checkOAuthAccount`)

```go
// 1. 检查 access_token 是否存在
if account.Credentials.AccessToken == "" {
    return "no access token"  // ❌ 不健康
}

// 2. 检查 token 是否过期
if account.ExpiresAt < time.Now() {
    return "access token expired"  // ❌ 不健康
}

// 3. 测试请求 claude.ai/api/organizations
req.Header.Set("Authorization", "Bearer " + access_token)
resp := GET("https://claude.ai/api/organizations")

// 4. 检查响应状态码
if resp.StatusCode == 401 || 403 {
    return "authentication failed: status 401/403"  // ❌ 不健康
}

if resp.StatusCode >= 400 {
    return "API error: status {code}"  // ❌ 不健康
}

// ✅ 所有检查通过 → 健康
```

**OAuth 账户不健康的原因:**
1. ❌ **access_token 为空** - 账户创建不完整
2. ❌ **token 过期** - `expires_at < now()`
3. ❌ **401/403 认证失败** - Token 被撤销或无效
4. ❌ **429 限流** - 请求过多
5. ❌ **5xx 服务器错误** - Claude.ai 服务问题
6. ❌ **网络错误** - 无法连接到 claude.ai

---

### 🔵 SessionKey 账户 (type=session_key)

#### 检查逻辑 (`checkSessionKeyAccount`)

```go
// 1. 检查 session_key 是否存在
if account.Credentials.SessionKey == "" {
    return "no session key"  // ❌ 不健康
}

// 2. 测试请求 claude.ai/api/organizations
req.Header.Set("Cookie", "sessionKey=" + session_key)
resp := GET("https://claude.ai/api/organizations")

// 3. 检查响应状态码
if resp.StatusCode == 401 || 403 {
    return "authentication failed: status 401/403"  // ❌ 不健康
}

if resp.StatusCode >= 400 {
    return "API error: status {code}"  // ❌ 不健康
}

// ✅ 所有检查通过 → 健康
```

**SessionKey 账户不健康的原因:**
1. ❌ **session_key 为空**
2. ❌ **401/403 认证失败** - SessionKey 过期或无效
3. ❌ **429 限流**
4. ❌ **5xx 服务器错误**
5. ❌ **网络错误**

---

### 🟢 API Key 账户 (type=api_key)

#### 检查逻辑 (`checkAPIKeyAccount`)

```go
// 只做基础验证，不发请求 (避免计费)
if account.Credentials.APIKey == "" {
    return "no API key"  // ❌ 不健康
}

if len(account.Credentials.APIKey) < 10 {
    return "invalid API key format"  // ❌ 不健康
}

// ✅ 格式检查通过 → 健康
```

**API Key 账户不健康的原因:**
1. ❌ **api_key 为空**
2. ❌ **api_key 格式无效** (长度 < 10)

---

## 健康检查失败后的影响

### 旧逻辑 (已被新逻辑替代)

```
健康检查失败
→ circuit.RecordFailure(accountID)
→ 连续失败 5 次
→ 熔断器打开 (Open)
→ IsAvailable() = false
→ 账户不可用 30 秒
```

### 🆕 新逻辑 (sub2api style)

```
健康检查失败
→ 仍记录到熔断器 (旧代码未删除)
→ 但不影响调度!

实际调度逻辑:
GetSchedulableAccounts()
→ SELECT * FROM accounts WHERE schedulable=1 AND status='active'
→ 不检查熔断器状态
→ 只看数据库字段 ✅
```

### 请求失败后的处理 (新逻辑核心)

```go
// 在 error_handler.go 中
ClassifyAndHandleError(resp, accountID):

switch resp.StatusCode:
case 429:  // Rate Limited
    // 设置限流恢复时间
    rate_limit_reset_at = now + 60秒
    schedulable = false
    // → 60秒后自动恢复 ✅

case 401, 403:  // Auth Error
    // 认证失败 - 需要人工修复
    status = 'error'
    schedulable = false
    error_message = 'authentication failed'
    // → 需要刷新 token 才能恢复

case 503:  // Service Unavailable
    // 临时过载
    overload_until = now + 10秒
    // → 10秒后自动恢复 ✅

case 5xx:  // Server Error
    // 服务器错误 - 不标记账户失败
    // 只切换到其他账户重试
```

---

## 常见不健康场景分析

### 场景1: Token 过期 (最常见)

```
OAuth 账户:
  expires_at: 2026-01-20 10:00:00
  当前时间: 2026-01-20 10:30:00

健康检查:
  ❌ IsExpired() = true
  ❌ 标记为不健康

解决方法:
  curl -X POST -H "X-Admin-Key: $KEY" \
    http://localhost:8080/api/account/{id}/refresh
```

### 场景2: 认证失败 (403)

```
请求 claude.ai/api/organizations
  ← 403 Forbidden

原因可能:
  1. Token 被撤销
  2. 账户被封禁
  3. IP 被限制

健康检查:
  ❌ authentication failed: status 403

新逻辑处理:
  → status = 'error'
  → schedulable = false
  → error_message = 'authentication failed'
  → 需要人工检查和修复
```

### 场景3: 限流 (429)

```
请求过多触发限流
  ← 429 Too Many Requests
  ← Retry-After: 60

健康检查:
  ❌ API error: status 429

新逻辑处理:
  → rate_limit_reset_at = now + 60秒
  → schedulable = false
  → temp_unschedulable_reason = 'rate_limited'

自动恢复:
  60秒后 IsSchedulable() = true ✅
```

### 场景4: 网络错误

```
无法连接 claude.ai
  错误: context deadline exceeded / connection refused

健康检查:
  ❌ request failed: {error}

影响:
  → 熔断器记录失败 (旧逻辑)
  → 但不影响 schedulable 字段
  → 下次健康检查可能恢复
```

---

## 如何查看账户健康状态

### 方法1: API 查询

```bash
# 查看所有账户
curl -s -H "X-Admin-Key: $KEY" http://localhost:8080/api/account/list | \
  jq '.[] | {
    name,
    status,
    schedulable,
    health_status,
    error_message,
    rate_limit_reset_at,
    temp_unschedulable_until
  }'
```

### 方法2: 查看日志

```bash
# 查看健康检查失败
tail -f logs/*.log | grep "health check failed"

# 查看熔断器状态变更
tail -f logs/*.log | grep "circuit breaker state changed"

# 查看账户被标记为 error
tail -f logs/*.log | grep "marking account as error"
```

### 方法3: 手动触发检查

```bash
# 检查特定账户
ACCOUNT_ID="acc_xxx"
curl -X POST -H "X-Admin-Key: $KEY" \
  http://localhost:8080/api/account/$ACCOUNT_ID/check | jq '.'
```

---

## 预防和恢复措施

### 预防措施

1. **设置合理的优先级**
   ```sql
   -- 高优先级账户 (priority=1)
   -- 低优先级账户 (priority=100)
   UPDATE accounts SET priority=1 WHERE name='main-account';
   ```

2. **配置多个账户** - 一个失败自动切换

3. **启用自动 token 刷新** - OAuth 账户会自动刷新

### 恢复措施

#### 1. 401/403 认证失败

```bash
# 刷新 OAuth token
curl -X POST -H "X-Admin-Key: $KEY" \
  http://localhost:8080/api/account/$ACCOUNT_ID/refresh

# 或重新登录
curl -X POST -H "X-Admin-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"account", "session_key":"sk-ant-sid01-..."}' \
  http://localhost:8080/api/account/oauth
```

#### 2. 429 限流

```bash
# 等待限流恢复 (60秒)
# 或者添加更多账户分担负载
```

#### 3. 手动清除临时标志

```bash
# 如果确认账户已恢复，手动清除标志
curl -X POST -H "X-Admin-Key: $KEY" \
  -H "Content-Type: application/json" \
  -d '{"status":"active","schedulable":true}' \
  http://localhost:8080/api/account/$ACCOUNT_ID

# 或直接操作数据库
sqlite3 ccproxy.db "UPDATE accounts SET
  status='active',
  schedulable=1,
  rate_limit_reset_at=NULL,
  temp_unschedulable_until=NULL,
  error_message=''
  WHERE id='$ACCOUNT_ID'"
```

---

## 总结

| 检查类型 | 触发频率 | 不健康原因 | 自动恢复 |
|---------|---------|-----------|---------|
| OAuth Token 过期 | 每5分钟 | `expires_at < now` | ✅ 自动刷新 |
| OAuth 401/403 | 每5分钟 | Token 无效 | ❌ 需人工修复 |
| SessionKey 401/403 | 每5分钟 | SessionKey 过期 | ❌ 需人工更新 |
| 429 限流 | 实时请求 | 请求过多 | ✅ 60秒后恢复 |
| 503 过载 | 实时请求 | 服务不可用 | ✅ 10秒后恢复 |
| 网络错误 | 每5分钟 | 连接失败 | ✅ 下次检查可能恢复 |

**关键点:**
- ✅ 新逻辑不依赖熔断器，只看数据库 `schedulable` 字段
- ✅ 限流和过载会自动恢复
- ❌ 认证失败需要手动修复
- 📊 通过日志和 API 可以实时监控健康状态
