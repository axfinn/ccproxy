import { useState } from 'react';
import { useConversations, useConversationSearch } from '@/hooks/useConversations';
import { useTokens } from '@/hooks/useTokens';
import { apiClient } from '@/api/client';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Download, Search, AlertCircle, ChevronLeft, ChevronRight, Trash2, MessageSquare } from 'lucide-react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Textarea } from '@/components/ui/textarea';
import type { ConversationContent } from '@/api/types';

export function Conversations() {
  const [page, setPage] = useState(0);
  const [tokenFilter, setTokenFilter] = useState('');
  const [searchQuery, setSearchQuery] = useState('');
  const [searchMode, setSearchMode] = useState(false);
  const [selectedConv, setSelectedConv] = useState<ConversationContent | null>(null);
  const [exporting, setExporting] = useState(false);

  const { data: tokensData } = useTokens();
  const { data, loading, error, refetch } = useConversations({
    token_id: tokenFilter || undefined,
    page,
    limit: 20,
  });

  const { data: searchData, loading: searchLoading } = useConversationSearch(
    tokenFilter || null,
    searchQuery,
    searchMode && searchQuery.length >= 2
  );

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr);
    return date.toLocaleString('zh-CN');
  };

  const truncate = (text: string, maxLen = 100) => {
    if (text.length <= maxLen) return text;
    return text.substring(0, maxLen) + '...';
  };

  const handleExport = async (format: 'json' | 'jsonl') => {
    try {
      setExporting(true);
      const blob = await apiClient.exportConversations({
        token_id: tokenFilter || undefined,
      }, format);

      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `conversations_${new Date().toISOString().split('T')[0]}.${format}`;
      a.click();
      window.URL.revokeObjectURL(url);
    } catch (err) {
      alert('导出失败: ' + (err instanceof Error ? err.message : '未知错误'));
    } finally {
      setExporting(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('确定要删除这条对话记录吗？')) return;

    try {
      await apiClient.deleteConversation(id);
      refetch();
      setSelectedConv(null);
    } catch (err) {
      alert('删除失败: ' + (err instanceof Error ? err.message : '未知错误'));
    }
  };

  const handleSearch = () => {
    if (!tokenFilter) {
      alert('请先选择一个 Token');
      return;
    }
    if (searchQuery.length < 2) {
      alert('搜索关键词至少 2 个字符');
      return;
    }
    setSearchMode(true);
  };

  const clearSearch = () => {
    setSearchMode(false);
    setSearchQuery('');
  };

  const displayConvs = searchMode
    ? searchData?.conversations ?? []
    : data?.conversations ?? [];

  const parseMessages = (json: string) => {
    try {
      return JSON.parse(json);
    } catch {
      return [];
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">对话记录</h1>
          <p className="text-muted-foreground">
            查看和搜索用户对话内容（需启用对话记录）
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => handleExport('json')} disabled={exporting || !tokenFilter}>
            <Download className="h-4 w-4 mr-2" />
            导出 JSON
          </Button>
          <Button variant="outline" onClick={() => handleExport('jsonl')} disabled={exporting || !tokenFilter}>
            <Download className="h-4 w-4 mr-2" />
            导出 JSONL
          </Button>
        </div>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {/* Filters and Search */}
      <Card>
        <CardHeader>
          <CardTitle>筛选和搜索</CardTitle>
          <CardDescription>选择 Token 并搜索对话内容</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-3">
            <div>
              <label className="text-sm font-medium mb-2 block">Token</label>
              <Select value={tokenFilter} onValueChange={(v) => { setTokenFilter(v); setSearchMode(false); }}>
                <SelectTrigger>
                  <SelectValue placeholder="选择 Token" />
                </SelectTrigger>
                <SelectContent>
                  {tokensData?.tokens
                    .filter(t => t.enable_conversation_logging)
                    .map((token) => (
                      <SelectItem key={token.id} value={token.id}>
                        {token.name} ({token.id.substring(0, 8)})
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>

            <div className="md:col-span-2">
              <label className="text-sm font-medium mb-2 block">搜索关键词</label>
              <div className="flex gap-2">
                <Input
                  placeholder="输入搜索关键词..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
                />
                <Button onClick={handleSearch} disabled={!tokenFilter || searchQuery.length < 2}>
                  <Search className="h-4 w-4 mr-2" />
                  搜索
                </Button>
                {searchMode && (
                  <Button variant="outline" onClick={clearSearch}>
                    清除
                  </Button>
                )}
              </div>
            </div>
          </div>

          {searchMode && (
            <div className="mt-4">
              <Badge variant="secondary">
                搜索模式："{searchQuery}" - 找到 {searchData?.conversations.length ?? 0} 条结果
              </Badge>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Conversations List */}
      <Card>
        <CardHeader>
          <CardTitle>对话列表</CardTitle>
          <CardDescription>
            {searchMode
              ? `搜索结果: ${searchData?.conversations.length ?? 0} 条`
              : `共 ${data?.total ?? 0} 条记录，当前第 ${page + 1} 页`}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {(loading || searchLoading) ? (
            <div className="text-center py-8 text-muted-foreground">加载中...</div>
          ) : !tokenFilter ? (
            <div className="text-center py-8 text-muted-foreground">
              <MessageSquare className="h-12 w-12 mx-auto mb-4 opacity-50" />
              <p>请先选择一个 Token 以查看对话记录</p>
            </div>
          ) : displayConvs.length > 0 ? (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>时间</TableHead>
                    <TableHead>Prompt 预览</TableHead>
                    <TableHead>Completion 预览</TableHead>
                    <TableHead>压缩</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {displayConvs.map((conv) => (
                    <TableRow
                      key={conv.id}
                      className="cursor-pointer hover:bg-muted/50"
                      onClick={() => setSelectedConv(conv)}
                    >
                      <TableCell className="text-xs whitespace-nowrap">
                        {formatDate(conv.created_at)}
                      </TableCell>
                      <TableCell className="text-xs max-w-xs">
                        <div className="truncate">{truncate(conv.prompt, 80)}</div>
                      </TableCell>
                      <TableCell className="text-xs max-w-xs">
                        <div className="truncate">{truncate(conv.completion, 80)}</div>
                      </TableCell>
                      <TableCell>
                        {conv.is_compressed ? (
                          <Badge variant="secondary">已压缩</Badge>
                        ) : (
                          <Badge variant="outline">未压缩</Badge>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              {/* Pagination (only in list mode) */}
              {!searchMode && data && (
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
              )}
            </>
          ) : (
            <div className="text-center py-8 text-muted-foreground">
              暂无对话记录
              {tokenFilter && (
                <p className="text-xs mt-2">
                  请确保该 Token 已启用对话记录功能
                </p>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Conversation Detail Dialog */}
      <Dialog open={!!selectedConv} onOpenChange={() => setSelectedConv(null)}>
        <DialogContent className="max-w-4xl max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>对话详情</DialogTitle>
            <DialogDescription>
              创建时间: {selectedConv && formatDate(selectedConv.created_at)}
            </DialogDescription>
          </DialogHeader>
          {selectedConv && (
            <div className="space-y-4">
              {selectedConv.system_prompt && (
                <div>
                  <div className="text-sm font-medium mb-2">系统提示词</div>
                  <div className="bg-muted p-3 rounded text-sm">
                    {selectedConv.system_prompt}
                  </div>
                </div>
              )}

              {parseMessages(selectedConv.messages_json).length > 0 && (
                <div>
                  <div className="text-sm font-medium mb-2">对话历史</div>
                  <div className="space-y-2 max-h-60 overflow-y-auto">
                    {parseMessages(selectedConv.messages_json).map((msg: any, i: number) => (
                      <div key={i} className="bg-muted p-3 rounded text-sm">
                        <Badge className="mb-2">{msg.role}</Badge>
                        <div className="whitespace-pre-wrap">{JSON.stringify(msg.content).slice(1, -1)}</div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              <div>
                <div className="text-sm font-medium mb-2">用户输入</div>
                <Textarea
                  value={selectedConv.prompt}
                  readOnly
                  className="min-h-[100px] font-mono text-xs"
                />
              </div>

              <div>
                <div className="text-sm font-medium mb-2">模型输出</div>
                <Textarea
                  value={selectedConv.completion}
                  readOnly
                  className="min-h-[200px] font-mono text-xs"
                />
              </div>

              <div className="flex justify-between items-center pt-4 border-t">
                <div className="text-xs text-muted-foreground">
                  ID: {selectedConv.id}
                  {selectedConv.is_compressed && (
                    <Badge variant="secondary" className="ml-2">已压缩</Badge>
                  )}
                </div>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => handleDelete(selectedConv.id)}
                >
                  <Trash2 className="h-4 w-4 mr-2" />
                  删除
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
