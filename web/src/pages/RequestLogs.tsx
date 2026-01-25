import { useState } from 'react';
import { useRequestLogs } from '@/hooks/useRequestLogs';
import { useTokens } from '@/hooks/useTokens';
import { apiClient } from '@/api/client';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Download, AlertCircle, CheckCircle2, XCircle, ChevronLeft, ChevronRight } from 'lucide-react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import type { RequestLog } from '@/api/types';

export function RequestLogs() {
  const [page, setPage] = useState(0);
  const [tokenFilter, setTokenFilter] = useState('');
  const [modeFilter, setModeFilter] = useState('');
  const [successFilter, setSuccessFilter] = useState<boolean | undefined>();
  const [selectedLog, setSelectedLog] = useState<RequestLog | null>(null);
  const [exporting, setExporting] = useState(false);

  const { data: tokensData } = useTokens();
  const { data, loading, error } = useRequestLogs({
    token_id: tokenFilter || undefined,
    mode: modeFilter || undefined,
    success: successFilter,
    page,
    limit: 20,
  });

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr);
    return date.toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    });
  };

  const formatDuration = (ms?: number) => {
    if (!ms) return '-';
    if (ms >= 1000) return (ms / 1000).toFixed(2) + 's';
    return ms.toFixed(0) + 'ms';
  };

  const formatTokens = (tokens: number) => {
    if (tokens >= 1000) return (tokens / 1000).toFixed(1) + 'K';
    return tokens.toString();
  };

  const handleExport = async (format: 'csv' | 'json') => {
    try {
      setExporting(true);
      const blob = await apiClient.exportRequestLogs({
        token_id: tokenFilter || undefined,
        mode: modeFilter || undefined,
        success: successFilter,
      }, format);

      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `request_logs_${new Date().toISOString().split('T')[0]}.${format}`;
      a.click();
      window.URL.revokeObjectURL(url);
    } catch (err) {
      alert('导出失败: ' + (err instanceof Error ? err.message : '未知错误'));
    } finally {
      setExporting(false);
    }
  };

  const resetFilters = () => {
    setTokenFilter('');
    setModeFilter('');
    setSuccessFilter(undefined);
    setPage(0);
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">请求日志</h1>
          <p className="text-muted-foreground">
            查看所有 API 请求的详细日志
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => handleExport('csv')} disabled={exporting}>
            <Download className="h-4 w-4 mr-2" />
            导出 CSV
          </Button>
          <Button variant="outline" onClick={() => handleExport('json')} disabled={exporting}>
            <Download className="h-4 w-4 mr-2" />
            导出 JSON
          </Button>
        </div>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {/* Filters */}
      <Card>
        <CardHeader>
          <CardTitle>筛选</CardTitle>
          <CardDescription>按条件筛选请求日志</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-4">
            <div>
              <label className="text-sm font-medium mb-2 block">Token</label>
              <Select value={tokenFilter || 'all'} onValueChange={(v) => setTokenFilter(v === 'all' ? '' : v)}>
                <SelectTrigger>
                  <SelectValue placeholder="全部 Token" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部</SelectItem>
                  {tokensData?.tokens.map((token) => (
                    <SelectItem key={token.id} value={token.id}>
                      {token.name} ({token.id.substring(0, 8)})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div>
              <label className="text-sm font-medium mb-2 block">模式</label>
              <Select value={modeFilter || 'all'} onValueChange={(v) => setModeFilter(v === 'all' ? '' : v)}>
                <SelectTrigger>
                  <SelectValue placeholder="全部模式" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部</SelectItem>
                  <SelectItem value="web">Web</SelectItem>
                  <SelectItem value="api">API</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div>
              <label className="text-sm font-medium mb-2 block">状态</label>
              <Select
                value={successFilter === undefined ? 'all' : successFilter.toString()}
                onValueChange={(v) => setSuccessFilter(v === 'all' ? undefined : v === 'true')}
              >
                <SelectTrigger>
                  <SelectValue placeholder="全部状态" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部</SelectItem>
                  <SelectItem value="true">成功</SelectItem>
                  <SelectItem value="false">失败</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-end">
              <Button variant="outline" onClick={resetFilters} className="w-full">
                重置筛选
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Logs Table */}
      <Card>
        <CardHeader>
          <CardTitle>日志列表</CardTitle>
          <CardDescription>
            共 {data?.total ?? 0} 条记录，当前第 {page + 1} 页
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="text-center py-8 text-muted-foreground">加载中...</div>
          ) : data && data.logs.length > 0 ? (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>时间</TableHead>
                    <TableHead>用户</TableHead>
                    <TableHead>模式</TableHead>
                    <TableHead>模型</TableHead>
                    <TableHead className="text-right">Tokens</TableHead>
                    <TableHead className="text-right">耗时</TableHead>
                    <TableHead>状态</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.logs.map((log) => (
                    <TableRow
                      key={log.id}
                      className="cursor-pointer hover:bg-muted/50"
                      onClick={() => setSelectedLog(log)}
                    >
                      <TableCell className="text-xs">{formatDate(log.request_at)}</TableCell>
                      <TableCell className="font-mono text-xs">
                        {log.user_name} ({log.token_id.substring(0, 6)})
                      </TableCell>
                      <TableCell>
                        <Badge variant={log.mode === 'api' ? 'default' : 'secondary'}>
                          {log.mode.toUpperCase()}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs">{log.model}</TableCell>
                      <TableCell className="text-right">{formatTokens(log.total_tokens)}</TableCell>
                      <TableCell className="text-right">{formatDuration(log.duration_ms)}</TableCell>
                      <TableCell>
                        {log.success ? (
                          <Badge variant="default" className="gap-1">
                            <CheckCircle2 className="h-3 w-3" />
                            成功
                          </Badge>
                        ) : (
                          <Badge variant="destructive" className="gap-1">
                            <XCircle className="h-3 w-3" />
                            失败
                          </Badge>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              {/* Pagination */}
              <div className="flex items-center justify-between mt-4">
                <div className="text-sm text-muted-foreground">
                  显示 {page * 20 + 1} - {Math.min((page + 1) * 20, data.total)} 条，共 {data.total} 条
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage(p => Math.max(0, p - 1))}
                    disabled={page === 0}
                  >
                    <ChevronLeft className="h-4 w-4" />
                    上一页
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage(p => p + 1)}
                    disabled={(page + 1) * 20 >= data.total}
                  >
                    下一页
                    <ChevronRight className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            </>
          ) : (
            <div className="text-center py-8 text-muted-foreground">暂无日志数据</div>
          )}
        </CardContent>
      </Card>

      {/* Log Detail Dialog */}
      <Dialog open={!!selectedLog} onOpenChange={() => setSelectedLog(null)}>
        <DialogContent className="max-w-3xl max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>请求详情</DialogTitle>
            <DialogDescription>查看请求的完整信息</DialogDescription>
          </DialogHeader>
          {selectedLog && (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <div className="text-sm font-medium text-muted-foreground">请求 ID</div>
                  <div className="text-sm font-mono">{selectedLog.id}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Token ID</div>
                  <div className="text-sm font-mono">{selectedLog.token_id}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">用户名</div>
                  <div className="text-sm">{selectedLog.user_name}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">模式</div>
                  <div className="text-sm">
                    <Badge variant={selectedLog.mode === 'api' ? 'default' : 'secondary'}>
                      {selectedLog.mode.toUpperCase()}
                    </Badge>
                  </div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">模型</div>
                  <div className="text-sm">{selectedLog.model}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">流式响应</div>
                  <div className="text-sm">{selectedLog.stream ? '是' : '否'}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">请求时间</div>
                  <div className="text-sm">{formatDate(selectedLog.request_at)}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">响应时间</div>
                  <div className="text-sm">
                    {selectedLog.response_at ? formatDate(selectedLog.response_at) : '-'}
                  </div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">耗时</div>
                  <div className="text-sm">{formatDuration(selectedLog.duration_ms)}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">TTFT</div>
                  <div className="text-sm">{formatDuration(selectedLog.ttft_ms)}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Prompt Tokens</div>
                  <div className="text-sm">{selectedLog.prompt_tokens.toLocaleString()}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Completion Tokens</div>
                  <div className="text-sm">{selectedLog.completion_tokens.toLocaleString()}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Total Tokens</div>
                  <div className="text-sm font-bold">{selectedLog.total_tokens.toLocaleString()}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground">状态码</div>
                  <div className="text-sm">{selectedLog.status_code}</div>
                </div>
              </div>

              {selectedLog.error_message && (
                <div>
                  <div className="text-sm font-medium text-muted-foreground mb-2">错误信息</div>
                  <div className="text-sm text-red-600 bg-red-50 p-3 rounded border border-red-200">
                    {selectedLog.error_message}
                  </div>
                </div>
              )}

              {selectedLog.conversation_id && (
                <div>
                  <div className="text-sm font-medium text-muted-foreground">对话 ID</div>
                  <div className="text-sm font-mono">{selectedLog.conversation_id}</div>
                </div>
              )}
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
