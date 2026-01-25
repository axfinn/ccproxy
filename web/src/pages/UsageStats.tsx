import { useState } from 'react';
import { useGlobalStats, useRealtimeStats, useTopTokens, useTopModels } from '@/hooks/useUsageStats';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Activity, TrendingUp, Users, Zap, AlertCircle } from 'lucide-react';
import { Alert, AlertDescription } from '@/components/ui/alert';

export function UsageStats() {
  const [timeRange, setTimeRange] = useState<number>(7);
  const { data: globalStats, loading: globalLoading, error: globalError } = useGlobalStats(timeRange);
  const { data: realtimeStats, loading: realtimeLoading } = useRealtimeStats();
  const { data: topTokens, loading: tokensLoading } = useTopTokens(timeRange, 10);
  const { data: topModels, loading: modelsLoading } = useTopModels(timeRange);

  const formatNumber = (num: number) => {
    if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
    if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
    return num.toString();
  };

  const formatDuration = (ms: number) => {
    if (ms >= 1000) return (ms / 1000).toFixed(1) + 's';
    return ms.toFixed(0) + 'ms';
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">用量统计</h1>
          <p className="text-muted-foreground">
            系统用量概览和分析
          </p>
        </div>
        <Select value={timeRange.toString()} onValueChange={(v) => setTimeRange(Number(v))}>
          <SelectTrigger className="w-[180px]">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="1">今天</SelectItem>
            <SelectItem value="7">最近 7 天</SelectItem>
            <SelectItem value="30">最近 30 天</SelectItem>
            <SelectItem value="90">最近 90 天</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {globalError && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>{globalError}</AlertDescription>
        </Alert>
      )}

      {/* Realtime Stats */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">今日请求</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {realtimeLoading ? (
              <div className="text-2xl font-bold">...</div>
            ) : (
              <>
                <div className="text-2xl font-bold">{formatNumber(realtimeStats?.total_requests ?? 0)}</div>
                <p className="text-xs text-muted-foreground">
                  成功率 {(realtimeStats?.success_rate ?? 0).toFixed(1)}%
                </p>
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">今日 Token 使用</CardTitle>
            <Zap className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {realtimeLoading ? (
              <div className="text-2xl font-bold">...</div>
            ) : (
              <>
                <div className="text-2xl font-bold">{formatNumber(realtimeStats?.total_tokens ?? 0)}</div>
                <p className="text-xs text-muted-foreground">
                  平均响应 {formatDuration(realtimeStats?.avg_duration_ms ?? 0)}
                </p>
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">活跃用户</CardTitle>
            <Users className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {globalLoading ? (
              <div className="text-2xl font-bold">...</div>
            ) : (
              <>
                <div className="text-2xl font-bold">{globalStats?.active_tokens ?? 0}</div>
                <p className="text-xs text-muted-foreground">
                  总用户 {globalStats?.total_users ?? 0}
                </p>
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">总请求数</CardTitle>
            <TrendingUp className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {globalLoading ? (
              <div className="text-2xl font-bold">...</div>
            ) : (
              <>
                <div className="text-2xl font-bold">{formatNumber(globalStats?.total_requests ?? 0)}</div>
                <p className="text-xs text-muted-foreground">
                  {formatNumber(globalStats?.total_tokens ?? 0)} tokens
                </p>
              </>
            )}
          </CardContent>
        </Card>
      </div>

      {/* By Mode */}
      {globalStats?.by_mode && Object.keys(globalStats.by_mode).length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>按模式统计</CardTitle>
            <CardDescription>Web 模式和 API 模式的用量对比</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>模式</TableHead>
                  <TableHead className="text-right">请求数</TableHead>
                  <TableHead className="text-right">成功率</TableHead>
                  <TableHead className="text-right">Token 使用</TableHead>
                  <TableHead className="text-right">平均响应时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {Object.entries(globalStats.by_mode).map(([mode, stats]) => (
                  <TableRow key={mode}>
                    <TableCell className="font-medium">
                      <Badge variant={mode === 'api' ? 'default' : 'secondary'}>
                        {mode.toUpperCase()}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">{formatNumber(stats.request_count)}</TableCell>
                    <TableCell className="text-right">{stats.success_rate.toFixed(1)}%</TableCell>
                    <TableCell className="text-right">{formatNumber(stats.total_tokens)}</TableCell>
                    <TableCell className="text-right">{formatDuration(stats.avg_duration_ms)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        {/* Top Tokens */}
        <Card>
          <CardHeader>
            <CardTitle>Top Tokens</CardTitle>
            <CardDescription>使用量最高的 Token</CardDescription>
          </CardHeader>
          <CardContent>
            {tokensLoading ? (
              <div className="text-center py-8 text-muted-foreground">加载中...</div>
            ) : topTokens && topTokens.tokens.length > 0 ? (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Token ID</TableHead>
                    <TableHead className="text-right">请求数</TableHead>
                    <TableHead className="text-right">成功率</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {topTokens.tokens.map((token) => (
                    <TableRow key={token.token_id}>
                      <TableCell className="font-mono text-xs">
                        {token.token_id.substring(0, 8)}...
                      </TableCell>
                      <TableCell className="text-right">{formatNumber(token.total_requests)}</TableCell>
                      <TableCell className="text-right">
                        <Badge variant={token.success_rate > 95 ? 'default' : 'secondary'}>
                          {token.success_rate.toFixed(1)}%
                        </Badge>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            ) : (
              <div className="text-center py-8 text-muted-foreground">暂无数据</div>
            )}
          </CardContent>
        </Card>

        {/* Top Models */}
        <Card>
          <CardHeader>
            <CardTitle>Top Models</CardTitle>
            <CardDescription>使用量最高的模型</CardDescription>
          </CardHeader>
          <CardContent>
            {modelsLoading ? (
              <div className="text-center py-8 text-muted-foreground">加载中...</div>
            ) : topModels && topModels.models.length > 0 ? (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>模型</TableHead>
                    <TableHead className="text-right">请求数</TableHead>
                    <TableHead className="text-right">Tokens</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {topModels.models.map((model) => (
                    <TableRow key={model.model}>
                      <TableCell className="font-medium">{model.model}</TableCell>
                      <TableCell className="text-right">{formatNumber(model.total_requests)}</TableCell>
                      <TableCell className="text-right">{formatNumber(model.total_tokens)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            ) : (
              <div className="text-center py-8 text-muted-foreground">暂无数据</div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
