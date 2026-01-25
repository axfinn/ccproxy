import { Link } from 'react-router-dom';
import { useTokens } from '@/hooks/useTokens';
import { useSessions } from '@/hooks/useSessions';
import { useApiKeys } from '@/hooks/useApiKeys';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Key, Globe, Server, Activity, BarChart3, MessageSquare, FileText, ArrowRight } from 'lucide-react';

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

      {/* Quick Access to New Features */}
      <Card>
        <CardHeader>
          <CardTitle>统计和日志</CardTitle>
          <CardDescription>查看系统使用情况和详细日志</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-3">
            <Link to="/usage-stats" className="block">
              <Card className="cursor-pointer hover:bg-muted/50 transition-colors">
                <CardContent className="pt-6">
                  <div className="flex items-start gap-4">
                    <div className="p-2 bg-primary/10 rounded-lg">
                      <BarChart3 className="h-6 w-6 text-primary" />
                    </div>
                    <div className="flex-1">
                      <h3 className="font-semibold mb-1">用量统计</h3>
                      <p className="text-xs text-muted-foreground mb-2">
                        Token 使用量、请求数、成功率等统计数据
                      </p>
                      <Button variant="ghost" size="sm" className="p-0 h-auto">
                        查看详情 <ArrowRight className="ml-1 h-3 w-3" />
                      </Button>
                    </div>
                  </div>
                </CardContent>
              </Card>
            </Link>

            <Link to="/conversations" className="block">
              <Card className="cursor-pointer hover:bg-muted/50 transition-colors">
                <CardContent className="pt-6">
                  <div className="flex items-start gap-4">
                    <div className="p-2 bg-primary/10 rounded-lg">
                      <MessageSquare className="h-6 w-6 text-primary" />
                    </div>
                    <div className="flex-1">
                      <h3 className="font-semibold mb-1">对话记录</h3>
                      <p className="text-xs text-muted-foreground mb-2">
                        查看和搜索用户对话内容，支持导出
                      </p>
                      <Button variant="ghost" size="sm" className="p-0 h-auto">
                        查看详情 <ArrowRight className="ml-1 h-3 w-3" />
                      </Button>
                    </div>
                  </div>
                </CardContent>
              </Card>
            </Link>

            <Link to="/request-logs" className="block">
              <Card className="cursor-pointer hover:bg-muted/50 transition-colors">
                <CardContent className="pt-6">
                  <div className="flex items-start gap-4">
                    <div className="p-2 bg-primary/10 rounded-lg">
                      <FileText className="h-6 w-6 text-primary" />
                    </div>
                    <div className="flex-1">
                      <h3 className="font-semibold mb-1">请求日志</h3>
                      <p className="text-xs text-muted-foreground mb-2">
                        详细的 API 请求日志，包含 Token 使用和性能数据
                      </p>
                      <Button variant="ghost" size="sm" className="p-0 h-auto">
                        查看详情 <ArrowRight className="ml-1 h-3 w-3" />
                      </Button>
                    </div>
                  </div>
                </CardContent>
              </Card>
            </Link>
          </div>
        </CardContent>
      </Card>

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
