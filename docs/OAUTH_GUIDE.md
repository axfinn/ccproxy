# OAuth 账号管理指南

## 概述

CCProxy 现已支持 OAuth 账号管理，允许您通过 Claude 的 sessionKey 自动获取 OAuth access token。这样可以获得更好的稳定性和安全性。

## 架构变更

### 账号类型

现在支持三种账号类型：

1. **OAuth 账号** (`oauth`): 通过 sessionKey 自动获取的 OAuth token
   - 自动刷新 token
   - 更长的过期时间
   - 更好的安全性

2. **Session Key 账号** (`session_key`): 传统的 sessionKey 方式
   - 兼容旧版本
   - 直接使用 sessionKey 访问 claude.ai

3. **API Key 账号** (`api_key`): 直接使用 API Key（未来支持）

### 数据库迁移

首次启动时，系统会自动将旧的 `sessions` 表数据迁移到新的 `accounts` 表，并将类型设置为 `session_key`。迁移是安全的，不会丢失数据。

## API 端点

### 1. OAuth 登录

通过 sessionKey 自动完成 3 步 OAuth 流程：

```bash
POST /api/account/oauth
X-Admin-Key: your-admin-key
Content-Type: application/json

{
  "session_key": "sk-ant-sid01-xxxxx",
  "name": "My OAuth Account"
}
```

响应：
```json
{
  "account_id": "acc_xxxxxxxx",
  "organization_id": "org-xxxxxxxx",
  "access_token": "oauth-xxxxx",
  "refresh_token": "oauth-refresh-xxxxx",
  "expires_at": "2026-01-23T14:00:00Z"
}
```

### 2. 创建 Session Key 账号（兼容旧版本）

```bash
POST /api/account/sessionkey
X-Admin-Key: your-admin-key
Content-Type: application/json

{
  "name": "My Session Account",
  "session_key": "sk-ant-sid01-xxxxx",
  "organization_id": "org-xxxxx"
}
```

### 3. 列出所有账号

```bash
GET /api/account/list
X-Admin-Key: your-admin-key
```

### 4. 获取账号详情

```bash
GET /api/account/{account_id}
X-Admin-Key: your-admin-key
```

### 5. 更新账号

```bash
PUT /api/account/{account_id}
X-Admin-Key: your-admin-key
Content-Type: application/json

{
  "name": "New Name",
  "is_active": true
}
```

### 6. 手动刷新 OAuth Token

```bash
POST /api/account/{account_id}/refresh
X-Admin-Key: your-admin-key
```

### 7. 健康检查

```bash
POST /api/account/{account_id}/check
X-Admin-Key: your-admin-key
```

### 8. 停用账号

```bash
POST /api/account/{account_id}/deactivate
X-Admin-Key: your-admin-key
```

### 9. 删除账号

```bash
DELETE /api/account/{account_id}
X-Admin-Key: your-admin-key
```

## OAuth 工作流程

### 自动 OAuth 登录（3步流程）

当您使用 `POST /api/account/oauth` 时，系统会自动完成以下步骤：

1. **获取组织 UUID**
   ```
   GET https://claude.ai/api/organizations
   Cookie: sessionKey=sk-ant-sid01-xxxxx
   ```

2. **获取授权码（使用 PKCE）**
   ```
   POST https://claude.ai/v1/oauth/{org_uuid}/authorize
   {
     "response_type": "code",
     "client_id": "claude-web-oauth-pkce",
     "code_challenge": "xxx",
     "code_challenge_method": "S256"
   }
   ```

3. **交换访问令牌**
   ```
   POST https://api.anthropic.com/v1/oauth/token
   {
     "grant_type": "authorization_code",
     "code": "xxx",
     "code_verifier": "xxx"
   }
   ```

### 自动 Token 刷新

系统会在以下情况自动刷新 token：

1. Token 剩余时间少于 5 分钟时
2. 健康检查失败（返回 401）时
3. 手动调用 refresh 端点时

## 反检测特性

### 浏览器指纹模拟

所有请求都使用真实的浏览器指纹：

- User-Agent: Chrome 131 (2026年最新版本)
- Client Hints (Sec-Ch-Ua-*)
- Sec-Fetch-* 安全头
- 完整的 Accept-Language 头

### OAuth 身份隔离

OAuth 账号使用 Bearer token 而不是 Cookie，提供更好的身份隔离：

```
Authorization: Bearer oauth-xxxxx
anthropic-beta: oauth-2025-04-20
```

## 测试脚本

项目包含了一个测试脚本 `test-oauth-account.sh`，可以测试所有账号管理功能：

```bash
./test-oauth-account.sh
```

## 使用建议

1. **推荐使用 OAuth 登录**：相比直接使用 sessionKey，OAuth 提供更长的有效期和自动刷新
2. **定期健康检查**：建议每小时运行一次健康检查
3. **监控 error_count**：如果错误计数持续增长，可能需要重新登录
4. **多账号轮换**：可以添加多个账号，系统会自动选择健康的账号使用

## 兼容性

- 完全向后兼容旧的 session 管理 API（`/api/session/*`）
- 数据库自动迁移，无需手动操作
- 支持混合使用 OAuth 和 SessionKey 账号

## 故障排查

### OAuth 登录失败

1. 检查 sessionKey 是否有效
2. 确认您有访问该组织的权限
3. 查看日志中的详细错误信息

### Token 刷新失败

1. refresh_token 可能已过期，需要重新登录
2. 账号可能被禁用
3. 检查网络连接

### 健康检查失败

1. 正常现象，测试 sessionKey 通常会失败
2. OAuth 账号检查失败可能表示 token 过期
3. 系统会自动尝试刷新 token

## 下一步

考虑添加：
- Admin UI 界面支持
- 自动健康检查后台任务
- 账号使用统计
- 智能负载均衡
