import { useTokens } from '@/hooks/useTokens';
import { useSessions } from '@/hooks/useSessions';
import { useApiKeys } from '@/hooks/useApiKeys';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Key, Globe, Server, Activity } from 'lucide-react';

export function Dashboard() {
  const { data: tokenData, isLoading: tokensLoading } = useTokens();
  const { data: sessionData, isLoading: sessionsLoading } = useSessions();
  const { data: keyData, isLoading: keysLoading } = useApiKeys();

  const validTokens = tokenData?.tokens.filter(t => t.is_valid).length ?? 0;
  const totalTokens = tokenData?.tokens.length ?? 0;
  const activeSessions = sessionData?.sessions.filter(s => s.is_active).length ?? 0;
  const totalSessions = sessionData?.sessions.length ?? 0;
  const healthyKeys = keyData?.healthy_keys ?? 0;
  const totalKeys = keyData?.total_keys ?? 0;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">控制台</h1>
        <p className="text-muted-foreground">
          CCProxy 实例概览
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">有效令牌</CardTitle>
            <Key className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {tokensLoading ? (
              <div className="text-2xl font-bold">...</div>
            ) : (
              <>
                <div className="text-2xl font-bold">{validTokens}</div>
                <p className="text-xs text-muted-foreground">
                  共 {totalTokens} 个令牌
                </p>
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Web 会话</CardTitle>
            <Globe className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {sessionsLoading ? (
              <div className="text-2xl font-bold">...</div>
            ) : (
              <>
                <div className="text-2xl font-bold">{activeSessions}</div>
                <p className="text-xs text-muted-foreground">
                  共 {totalSessions} 个会话
                </p>
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">API 密钥</CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {keysLoading ? (
              <div className="text-2xl font-bold">...</div>
            ) : (
              <>
                <div className="text-2xl font-bold">{healthyKeys}</div>
                <p className="text-xs text-muted-foreground">
                  共 {totalKeys} 个密钥健康
                </p>
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">系统状态</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              <Badge variant="success">运行中</Badge>
            </div>
            <p className="text-xs text-muted-foreground">
              所有服务正常
            </p>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>最近令牌</CardTitle>
            <CardDescription>最近生成的令牌</CardDescription>
          </CardHeader>
          <CardContent>
            {tokensLoading ? (
              <p className="text-muted-foreground">加载中...</p>
            ) : tokenData?.tokens.length === 0 ? (
              <p className="text-muted-foreground">暂无令牌</p>
            ) : (
              <div className="space-y-2">
                {tokenData?.tokens.slice(0, 5).map((token) => (
                  <div
                    key={token.id}
                    className="flex items-center justify-between p-2 rounded-md bg-muted/50"
                  >
                    <div>
                      <p className="font-medium">{token.name}</p>
                      <p className="text-xs text-muted-foreground">
                        模式: {token.mode === 'both' ? '全部' : token.mode === 'web' ? 'Web' : 'API'}
                      </p>
                    </div>
                    <Badge variant={token.is_valid ? 'success' : 'secondary'}>
                      {token.is_valid ? '有效' : '无效'}
                    </Badge>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>活跃会话</CardTitle>
            <CardDescription>当前活跃的 Web 会话</CardDescription>
          </CardHeader>
          <CardContent>
            {sessionsLoading ? (
              <p className="text-muted-foreground">加载中...</p>
            ) : sessionData?.sessions.filter(s => s.is_active).length === 0 ? (
              <p className="text-muted-foreground">暂无活跃会话</p>
            ) : (
              <div className="space-y-2">
                {sessionData?.sessions
                  .filter(s => s.is_active)
                  .slice(0, 5)
                  .map((session) => (
                    <div
                      key={session.id}
                      className="flex items-center justify-between p-2 rounded-md bg-muted/50"
                    >
                      <div>
                        <p className="font-medium">{session.name}</p>
                        <p className="text-xs text-muted-foreground">
                          {session.organization_id || '无组织'}
                        </p>
                      </div>
                      <Badge variant="success">活跃</Badge>
                    </div>
                  ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
