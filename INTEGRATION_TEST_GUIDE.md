# 集成测试指南 - ccproxy 思考块和重试改进

## 手动测试场景

### 场景 1: 测试内容数组支持

**目标**: 验证系统能够正确处理包含思考块的 Anthropic API 响应

#### 请求样例
```bash
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "model": "claude-3-7-sonnet-20250219",
    "max_tokens": 1024,
    "thinking": {
      "type": "enabled",
      "budget_tokens": 5000
    },
    "messages": [
      {
        "role": "user",
        "content": "Solve this problem: What is 2+2?"
      }
    ]
  }'
```

#### 预期结果
- ✅ 请求被 FilterThinkingBlocks 处理
- ✅ 响应包含思考块和文本响应
- ✅ 无 JSON 解析错误
- ✅ 200 OK 状态码

#### 验证日志
```log
# 应该看到成功的响应处理
# 不应该看到: "cannot unmarshal array into Go struct field"
```

---

### 场景 2: 测试无签名思考块的过滤

**目标**: 验证系统在重试时能正确转换无签名思考块

#### 模拟条件
- 初始请求包含无签名思考块
- Anthropic API 返回 400 Bad Request（无效签名）
- 系统应该 **不重试**，而是直接返回错误

#### 预期行为
```
请求 1: 包含思考块 → 400 Bad Request (无效签名)
  └─ ShouldRetry? 否 (400 错误) ✅
  └─ 直接返回 400 给客户端
  └─ 不触发重试或账户切换
```

#### 验证日志
```log
# 不应该看到:
"retrying after backoff"          # ❌ 应该被阻止
"switching account"                # ❌ 应该被阻止
"no available accounts"            # ❌ 应该不发生

# 应该看到:
"ShouldRetry return false for 400" # ✅ 显式拒绝重试
```

---

### 场景 3: 测试 500 错误的重试和账户切换

**目标**: 验证真正的服务器错误仍然会触发重试

#### 模拟条件
- Anthropic API 返回 500 Internal Server Error
- 系统应该 **重试**
- 系统应该 **不切换账户**（因为是服务器故障）

#### 预期行为
```
请求 1: 某个账户 → 500 Internal Server Error
  ├─ ShouldRetry? 是 (5xx 错误) ✅
  └─ 重试 (使用同一账户)
  
请求 2: 同一账户 → 500 Internal Server Error
  ├─ ShouldRetry? 是 (5xx 错误) ✅
  └─ 重试 (使用同一账户)
  
请求 3: 同一账户 → 成功
  └─ 返回响应
```

#### 验证日志
```log
"retrying after backoff"         # ✅ 应该看到
"backoff_duration"               # ✅ 显示退避时间

# 不应该看到:
"switching account"              # ❌ 500 不应切换账户
```

---

### 场景 4: 测试 429 速率限制的账户切换

**目标**: 验证速率限制会触发账户切换

#### 模拟条件
- 账户 1 返回 429 Too Many Requests
- 系统切换到账户 2
- 账户 2 返回成功

#### 预期行为
```
请求 1: 账户 1 → 429 Too Many Requests
  ├─ ShouldRetry? 是 ✅
  ├─ ShouldSwitchAccount? 是 ✅
  └─ 切换到账户 2
  
请求 2: 账户 2 → 200 OK
  └─ 返回响应
```

#### 验证日志
```log
"retrying after backoff"         # ✅ 重试
"switching account"              # ✅ 切换账户
"selected_account"               # ✅ 显示新账户
```

---

### 场景 5: 测试会话粘性（Sticky Sessions）

**目标**: 验证 metadata.user_id 用于会话粘性

#### 请求样例
```bash
# 请求包含 metadata.user_id
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "model": "claude-3-7-sonnet-20250219",
    "metadata": {
      "user_id": "user123"
    },
    "messages": [
      {
        "role": "user",
        "content": "Hello"
      }
    ]
  }'
```

#### 预期行为
- ✅ 首次请求使用 user_id 的哈希选择账户
- ✅ 同一 user_id 的后续请求使用同一账户
- ✅ 不同 user_id 可能使用不同账户

#### 验证日志
```log
"bound sticky session"           # ✅ 首次绑定
"session_hash"                   # ✅ 显示会话哈希
"selected_account"               # ✅ 一致的账户选择
```

---

### 场景 6: 测试 403 权限错误的账户切换

**目标**: 验证权限错误会导致账户切换

#### 预期行为
```
请求 1: 账户 1 → 403 Forbidden
  ├─ ShouldRetry? 否 (4xx 错误) ✅
  ├─ ShouldSwitchAccount? 是 ✅
  └─ 切换到账户 2
  
请求 2: 账户 2 → 200 OK
  └─ 返回响应
```

#### 验证日志
```log
# 应该看到:
"switching account"              # ✅ 切换账户
"account_1: 403"                 # ✅ 记录失败账户
"selected_account: account_2"    # ✅ 新选中的账户
```

---

## 日志验证清单

### 应该消失的错误模式

如果看到以下错误模式，说明问题未解决：

```log
❌ [ERROR] json: cannot unmarshal array into Go struct field
   → 解决方案: extractTextFromContent 和 interface{} 支持

❌ [INFO] retrying after backoff (for 400 errors)
   → 解决方案: ShouldRetry 拒绝 400

❌ [INFO] switching account (for 400 errors)
   → 解决方案: ShouldSwitchAccount 拒绝 400

❌ [WARN] circuit breaker state changed: closed → open
   (after "no available accounts" messages)
   → 解决方案: 减少因 400 导致的假性故障
```

### 应该看到的改进模式

```log
✅ request_body_filtered=true     # 思考块被过滤
✅ ShouldRetry return false for 400  # 400 被正确拒绝
✅ bound sticky session           # 会话粘性工作
✅ selected_account               # 负载均衡工作
✅ retrying after backoff (for 5xx) # 只为真实故障重试
```

---

## 性能测试

### 基准测试命令

```bash
# 测试包含思考块的请求处理性能
# （与不包含思考块的请求对比）
go test -bench=BenchmarkFilter -benchmem ./internal/handler

# 测试重试策略性能
go test -bench=BenchmarkShouldRetry -benchmem ./internal/retry
```

### 预期性能指标
- 过滤操作：< 1ms（JSON 解析和转换）
- 重试判断：< 1μs（简单状态码检查）
- 整体开销：< 5% 相对于原始请求时间

---

## 故障排除指南

### 问题 1: 仍然看到解析错误

**症状**:
```
json: cannot unmarshal array into Go struct field
```

**排查步骤**:
1. 检查 proxy.go 中的 OpenAIMessage.Content 是否为 `interface{}`
2. 检查 ChatCompletions 是否调用了 FilterThinkingBlocks
3. 运行 `go test ./internal/handler -run TestFilter` 验证

---

### 问题 2: 400 错误仍在重试

**症状**:
```
retrying after backoff for error: 400 Bad Request
```

**排查步骤**:
1. 检查 policy.go 中 ShouldRetry 是否有 `case http.StatusBadRequest: return false`
2. 运行 `go test ./internal/retry -run TestShouldRetry` 验证
3. 检查响应状态码是否被正确读取

---

### 问题 3: 断路器仍在打开

**症状**:
```
circuit breaker state changed: closed → open
```

**排查步骤**:
1. 查看断路器打开前的日志
2. 计数 400 vs 5xx 错误比例
3. 400 错误应该不会增加故障计数
4. 运行 `go test ./internal/circuit` 验证断路器逻辑

---

## 回滚步骤（如果需要）

如果发现新的改进引入了问题，可以回滚到之前的版本：

```bash
# 查看最近的提交
git log --oneline -10

# 回滚到特定提交
git revert <commit-hash>

# 或者硬回滚（谨慎使用）
git reset --hard HEAD~1
```

---

## 后续优化建议

1. **监控指标**:
   - 添加 Prometheus 指标跟踪过滤操作
   - 统计不同类型的错误分布

2. **日志增强**:
   - 添加 `thinking_blocks_filtered` 字段
   - 添加 `retry_reason` 字段区分重试原因

3. **高级特性**:
   - 实现自适应速率限制（根据 429 频率）
   - 实现预测性账户预热（基于历史成功率）

4. **文档化**:
   - 添加 API 文档说明支持的 content 格式
   - 添加故障排除指南到 README
