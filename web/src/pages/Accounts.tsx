import { useState } from 'react';
import {
  useAccounts,
  useOAuthLogin,
  useCreateSessionKeyAccount,
  useDeleteAccount,
  useDeactivateAccount,
  useRefreshToken,
  useCheckHealth,
  type Account,
} from '@/hooks/useAccounts';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import { Textarea } from '@/components/ui/textarea';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
  Plus,
  Trash2,
  Power,
  AlertCircle,
  RefreshCw,
  Activity,
  Info,
  Key,
  Loader2,
} from 'lucide-react';

export function Accounts() {
  const { data: accounts, isLoading, error } = useAccounts();
  const oauthLogin = useOAuthLogin();
  const createSessionKey = useCreateSessionKeyAccount();
  const deleteAccount = useDeleteAccount();
  const deactivateAccount = useDeactivateAccount();
  const refreshToken = useRefreshToken();
  const checkHealth = useCheckHealth();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [accountType, setAccountType] = useState<'oauth' | 'session_key'>('oauth');
  const [formData, setFormData] = useState({
    name: '',
    session_key: '',
    organization_id: '',
  });
  const [healthCheckResult, setHealthCheckResult] = useState<{ [key: string]: any }>({});

  const handleOAuthLogin = async () => {
    try {
      const result = await oauthLogin.mutateAsync({
        session_key: formData.session_key,
        name: formData.name,
      });
      alert(`OAuth 登录成功！\n账号 ID: ${result.account_id}\n过期时间: ${new Date(result.expires_at).toLocaleString()}`);
      setFormData({ name: '', session_key: '', organization_id: '' });
      setDialogOpen(false);
    } catch (err: any) {
      alert(`OAuth 登录失败: ${err.message}`);
    }
  };

  const handleCreateSessionKey = async () => {
    try {
      await createSessionKey.mutateAsync({
        name: formData.name,
        session_key: formData.session_key,
        organization_id: formData.organization_id || undefined,
      });
      setFormData({ name: '', session_key: '', organization_id: '' });
      setDialogOpen(false);
    } catch (err: any) {
      alert(`创建账号失败: ${err.message}`);
    }
  };

  const handleDelete = async (id: string) => {
    if (confirm('确定要删除此账号吗？')) {
      try {
        await deleteAccount.mutateAsync(id);
      } catch (err: any) {
        alert(`删除失败: ${err.message}`);
      }
    }
  };

  const handleDeactivate = async (id: string) => {
    if (confirm('确定要停用此账号吗？')) {
      try {
        await deactivateAccount.mutateAsync(id);
      } catch (err: any) {
        alert(`停用失败: ${err.message}`);
      }
    }
  };

  const handleRefresh = async (id: string) => {
    try {
      const result = await refreshToken.mutateAsync(id);
      alert(`Token 刷新成功！\n过期时间: ${new Date(result.expires_at).toLocaleString()}`);
    } catch (err: any) {
      alert(`刷新失败: ${err.message}`);
    }
  };

  const handleHealthCheck = async (id: string) => {
    try {
      const result = await checkHealth.mutateAsync(id);
      setHealthCheckResult({ ...healthCheckResult, [id]: result });
      setTimeout(() => {
        setHealthCheckResult((prev) => {
          const newState = { ...prev };
          delete newState[id];
          return newState;
        });
      }, 5000);
    } catch (err: any) {
      setHealthCheckResult({ ...healthCheckResult, [id]: { status: 'unhealthy', message: err.message } });
      setTimeout(() => {
        setHealthCheckResult((prev) => {
          const newState = { ...prev };
          delete newState[id];
          return newState;
        });
      }, 5000);
    }
  };

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return '-';
    return new Date(dateStr).toLocaleString('zh-CN');
  };

  const getAccountTypeBadge = (type: string) => {
    switch (type) {
      case 'oauth':
        return <Badge variant="default">OAuth</Badge>;
      case 'session_key':
        return <Badge variant="secondary">Session Key</Badge>;
      case 'api_key':
        return <Badge variant="outline">API Key</Badge>;
      default:
        return <Badge variant="secondary">{type}</Badge>;
    }
  };

  const getHealthBadge = (status: string) => {
    switch (status) {
      case 'healthy':
        return <Badge variant="success">健康</Badge>;
      case 'unhealthy':
        return <Badge variant="destructive">不健康</Badge>;
      default:
        return <Badge variant="secondary">未知</Badge>;
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">账号管理</h1>
          <p className="text-muted-foreground">
            管理 OAuth 账号和 Session Key 账号
          </p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              添加账号
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-[600px]">
            <DialogHeader>
              <DialogTitle>添加新账号</DialogTitle>
              <DialogDescription>
                选择账号类型并填写相关信息
              </DialogDescription>
            </DialogHeader>

            <Tabs value={accountType} onValueChange={(v) => setAccountType(v as any)}>
              <TabsList className="grid w-full grid-cols-2">
                <TabsTrigger value="oauth">OAuth 登录（推荐）</TabsTrigger>
                <TabsTrigger value="session_key">Session Key</TabsTrigger>
              </TabsList>

              <TabsContent value="oauth" className="space-y-4 mt-4">
                <Alert>
                  <Key className="h-4 w-4" />
                  <AlertTitle>OAuth 自动登录</AlertTitle>
                  <AlertDescription className="mt-2">
                    <ul className="list-disc list-inside space-y-1 text-sm">
                      <li>使用 sessionKey 自动完成 OAuth 认证流程</li>
                      <li>获得更长的有效期和自动刷新功能</li>
                      <li>更好的安全性和身份隔离</li>
                    </ul>
                  </AlertDescription>
                </Alert>

                <div className="space-y-2">
                  <Label htmlFor="oauth-name">账号名称</Label>
                  <Input
                    id="oauth-name"
                    placeholder="例如: 我的 OAuth 账号"
                    value={formData.name}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="oauth-session">Session Key</Label>
                  <Textarea
                    id="oauth-session"
                    placeholder="从 claude.ai 获取的 sessionKey"
                    value={formData.session_key}
                    onChange={(e) => setFormData({ ...formData, session_key: e.target.value })}
                    rows={3}
                  />
                  <p className="text-xs text-muted-foreground">
                    系统将自动使用此 sessionKey 完成 OAuth 认证流程
                  </p>
                </div>
              </TabsContent>

              <TabsContent value="session_key" className="space-y-4 mt-4">
                <Alert>
                  <Info className="h-4 w-4" />
                  <AlertTitle>传统 Session Key 模式</AlertTitle>
                  <AlertDescription className="mt-2">
                    <p className="text-sm">直接使用 sessionKey 访问 claude.ai，兼容旧版本</p>
                  </AlertDescription>
                </Alert>

                <div className="space-y-2">
                  <Label htmlFor="sk-name">账号名称</Label>
                  <Input
                    id="sk-name"
                    placeholder="例如: 我的账号"
                    value={formData.name}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="sk-session">Session Key</Label>
                  <Textarea
                    id="sk-session"
                    placeholder="claude.ai 的 sessionKey Cookie"
                    value={formData.session_key}
                    onChange={(e) => setFormData({ ...formData, session_key: e.target.value })}
                    rows={3}
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="sk-org">组织 ID (可选)</Label>
                  <Input
                    id="sk-org"
                    placeholder="例如: uuid-of-organization"
                    value={formData.organization_id}
                    onChange={(e) => setFormData({ ...formData, organization_id: e.target.value })}
                  />
                </div>
              </TabsContent>
            </Tabs>

            <DialogFooter>
              <Button variant="outline" onClick={() => setDialogOpen(false)}>
                取消
              </Button>
              <Button
                onClick={accountType === 'oauth' ? handleOAuthLogin : handleCreateSessionKey}
                disabled={
                  !formData.name ||
                  !formData.session_key ||
                  oauthLogin.isPending ||
                  createSessionKey.isPending
                }
              >
                {oauthLogin.isPending || createSessionKey.isPending ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    处理中...
                  </>
                ) : (
                  accountType === 'oauth' ? 'OAuth 登录' : '添加账号'
                )}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {/* 功能说明 */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Info className="h-5 w-5" />
            账号类型说明
          </CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-2">
          <div className="space-y-2">
            <h4 className="font-semibold flex items-center gap-2">
              <Badge variant="default">OAuth</Badge>
              推荐使用
            </h4>
            <ul className="list-disc list-inside space-y-1 text-sm text-muted-foreground">
              <li>自动完成 3 步 OAuth 认证流程</li>
              <li>自动刷新 token，无需手动维护</li>
              <li>更长的有效期（通常几天到几周）</li>
              <li>更好的安全性和身份隔离</li>
              <li>支持健康检查和自动恢复</li>
            </ul>
          </div>

          <div className="space-y-2">
            <h4 className="font-semibold flex items-center gap-2">
              <Badge variant="secondary">Session Key</Badge>
              传统模式
            </h4>
            <ul className="list-disc list-inside space-y-1 text-sm text-muted-foreground">
              <li>直接使用 sessionKey 访问 claude.ai</li>
              <li>兼容旧版本配置</li>
              <li>适合临时测试使用</li>
              <li>有效期较短，需手动更新</li>
            </ul>
          </div>
        </CardContent>
      </Card>

      {/* 账号列表 */}
      <Card>
        <CardHeader>
          <CardTitle>所有账号</CardTitle>
          <CardDescription>
            管理所有类型的 Claude 账号
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
              <span className="ml-3 text-muted-foreground">加载中...</span>
            </div>
          ) : error ? (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>
                加载账号失败: {error.message}
              </AlertDescription>
            </Alert>
          ) : !accounts || accounts.length === 0 ? (
            <div className="text-center py-8">
              <p className="text-muted-foreground">
                暂无账号。点击上方"添加账号"开始使用。
              </p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>名称</TableHead>
                    <TableHead>类型</TableHead>
                    <TableHead>组织 ID</TableHead>
                    <TableHead>健康状态</TableHead>
                    <TableHead>统计</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>过期时间</TableHead>
                    <TableHead>最后使用</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {accounts.map((account: Account) => (
                    <TableRow key={account.id}>
                      <TableCell className="font-medium">{account.name}</TableCell>
                      <TableCell>{getAccountTypeBadge(account.type)}</TableCell>
                      <TableCell className="text-sm text-muted-foreground font-mono text-xs">
                        {account.organization_id ? account.organization_id.substring(0, 12) + '...' : '-'}
                      </TableCell>
                      <TableCell>
                        {getHealthBadge(account.health_status)}
                        {healthCheckResult[account.id] && (
                          <div className="text-xs mt-1">
                            {healthCheckResult[account.id].status === 'healthy' ? (
                              <span className="text-green-600">✓ {healthCheckResult[account.id].message}</span>
                            ) : (
                              <span className="text-red-600">✗ {healthCheckResult[account.id].message}</span>
                            )}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        <div className="flex gap-2">
                          <span className="text-green-600">✓{account.success_count}</span>
                          <span className="text-red-600">✗{account.error_count}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        {account.is_active ? (
                          <Badge variant="success">活跃</Badge>
                        ) : (
                          <Badge variant="secondary">已停用</Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {account.expires_at
                          ? formatDate(account.expires_at)
                          : '永不过期'}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDate(account.last_used_at)}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-1">
                          {account.is_active && (
                            <>
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => handleHealthCheck(account.id)}
                                disabled={checkHealth.isPending}
                                title="健康检查"
                              >
                                <Activity className="h-4 w-4" />
                              </Button>
                              {account.type === 'oauth' && (
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  onClick={() => handleRefresh(account.id)}
                                  disabled={refreshToken.isPending}
                                  title="刷新 Token"
                                >
                                  <RefreshCw className="h-4 w-4" />
                                </Button>
                              )}
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => handleDeactivate(account.id)}
                                disabled={deactivateAccount.isPending}
                                title="停用"
                              >
                                <Power className="h-4 w-4" />
                              </Button>
                            </>
                          )}
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleDelete(account.id)}
                            disabled={deleteAccount.isPending}
                            title="删除"
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      {/* 使用提示 */}
      <Card>
        <CardHeader>
          <CardTitle>使用建议</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="list-disc list-inside space-y-2 text-sm text-muted-foreground">
            <li><strong>推荐使用 OAuth 登录：</strong>相比直接使用 sessionKey，OAuth 提供更长的有效期和自动刷新</li>
            <li><strong>定期健康检查：</strong>建议每小时运行一次健康检查确保账号可用</li>
            <li><strong>监控错误计数：</strong>如果错误计数持续增长，可能需要重新登录</li>
            <li><strong>多账号轮换：</strong>可以添加多个账号，系统会自动选择健康的账号使用</li>
            <li><strong>安全提醒：</strong>Token 和 SessionKey 相当于登录凭证，请勿泄露给他人</li>
          </ul>
        </CardContent>
      </Card>
    </div>
  );
}
