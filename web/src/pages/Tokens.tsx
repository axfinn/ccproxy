import { useState } from 'react';
import { useTokens, useGenerateToken, useRevokeToken } from '@/hooks/useTokens';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Plus, Copy, Ban, CheckCircle, AlertCircle, BookOpen, Terminal, Code, Info, MessageSquare, Loader2, Eye } from 'lucide-react';

export function Tokens() {
  const { data, isLoading, error } = useTokens();
  const generateToken = useGenerateToken();
  const revokeToken = useRevokeToken();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [newToken, setNewToken] = useState<string | null>(null);
  const [formData, setFormData] = useState({
    name: '',
    mode: 'both' as 'web' | 'api' | 'both',
    expires_in: '720h',
  });
  const [copySuccess, setCopySuccess] = useState(false);
  const [copiedExample, setCopiedExample] = useState<string | null>(null);
  const [testDialogOpen, setTestDialogOpen] = useState(false);
  const [testingToken, setTestingToken] = useState<any>(null);
  const [testTokenInput, setTestTokenInput] = useState('');
  const [testResult, setTestResult] = useState<{ success: boolean; message: string; response?: string } | null>(null);
  const [isTesting, setIsTesting] = useState(false);
  const [viewTokenDialogOpen, setViewTokenDialogOpen] = useState(false);
  const [viewingToken, setViewingToken] = useState<any>(null);
  const [viewTokenInput, setViewTokenInput] = useState('');

  const handleGenerate = async () => {
    try {
      const result = await generateToken.mutateAsync({
        name: formData.name,
        mode: formData.mode,
        expires_in: formData.expires_in,
      });
      setNewToken(result.token);
    } catch (err) {
      console.error('生成令牌失败:', err);
    }
  };

  const handleCopy = async () => {
    if (newToken) {
      await navigator.clipboard.writeText(newToken);
      setCopySuccess(true);
      setTimeout(() => setCopySuccess(false), 2000);
    }
  };

  const handleCopyExample = async (text: string, id: string) => {
    await navigator.clipboard.writeText(text);
    setCopiedExample(id);
    setTimeout(() => setCopiedExample(null), 2000);
  };

  const handleRevoke = async (id: string) => {
    if (confirm('确定要撤销此令牌吗？')) {
      try {
        await revokeToken.mutateAsync(id);
      } catch (err) {
        console.error('撤销令牌失败:', err);
      }
    }
  };

  const resetDialog = () => {
    setNewToken(null);
    setFormData({ name: '', mode: 'both', expires_in: '720h' });
    setDialogOpen(false);
  };

  const handleTestToken = async (token: any) => {
    setTestingToken(token);
    setTestResult(null);
    setTestTokenInput('');
    setTestDialogOpen(true);
  };

  const executeTest = async () => {
    if (!testTokenInput.trim()) {
      setTestResult({
        success: false,
        message: '请输入令牌'
      });
      return;
    }

    setIsTesting(true);
    setTestResult(null);

    try {
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${testTokenInput.trim()}`,
        },
        body: JSON.stringify({
          model: 'claude-3-5-sonnet-20241022',
          messages: [
            {
              role: 'user',
              content: 'Hello! Please respond with exactly: Test successful'
            }
          ],
          max_tokens: 100,
          stream: false
        })
      });

      const data = await response.json();

      if (response.ok && data.choices && data.choices[0]) {
        const content = data.choices[0].message.content;
        setTestResult({
          success: true,
          message: '令牌测试成功！可以正常使用。',
          response: content
        });
      } else {
        setTestResult({
          success: false,
          message: `测试失败: ${data.error || '未知错误'}`,
        });
      }
    } catch (error: any) {
      setTestResult({
        success: false,
        message: `测试失败: ${error.message}`,
      });
    } finally {
      setIsTesting(false);
    }
  };

  const handleViewToken = (token: any) => {
    setViewingToken(token);
    setViewTokenInput('');
    setViewTokenDialogOpen(true);
  };

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleString('zh-CN');
  };

  const getModeText = (mode: string) => {
    switch (mode) {
      case 'both': return '全部';
      case 'web': return 'Web';
      case 'api': return 'API';
      default: return mode;
    }
  };

  const serverUrl = window.location.origin;

  const curlExample = `curl -X POST ${serverUrl}/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer YOUR_TOKEN" \\
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {"role": "user", "content": "你好"}
    ]
  }'`;

  const pythonExample = `import openai

client = openai.OpenAI(
    api_key="YOUR_TOKEN",
    base_url="${serverUrl}/v1"
)

response = client.chat.completions.create(
    model="claude-sonnet-4-20250514",
    messages=[
        {"role": "user", "content": "你好"}
    ]
)

print(response.choices[0].message.content)`;

  const nodeExample = `import OpenAI from 'openai';

const client = new OpenAI({
  apiKey: 'YOUR_TOKEN',
  baseURL: '${serverUrl}/v1'
});

const response = await client.chat.completions.create({
  model: 'claude-sonnet-4-20250514',
  messages: [
    { role: 'user', content: '你好' }
  ]
});

console.log(response.choices[0].message.content);`;

  const streamExample = `curl -X POST ${serverUrl}/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer YOUR_TOKEN" \\
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [{"role": "user", "content": "写一首诗"}],
    "stream": true
  }'`;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">令牌管理</h1>
          <p className="text-muted-foreground">
            管理 API 访问的 JWT 令牌
          </p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={(open) => {
          if (!open) resetDialog();
          else setDialogOpen(true);
        }}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              生成令牌
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-[500px]">
            <DialogHeader>
              <DialogTitle>
                {newToken ? '令牌已生成' : '生成新令牌'}
              </DialogTitle>
              <DialogDescription>
                {newToken
                  ? '请立即复制令牌，此令牌将不会再次显示。'
                  : '创建新的 JWT 令牌用于 API 访问。'}
              </DialogDescription>
            </DialogHeader>

            {newToken ? (
              <div className="space-y-4">
                <Alert>
                  <AlertCircle className="h-4 w-4" />
                  <AlertDescription>
                    请妥善保存此令牌，关闭后将无法再次查看。
                  </AlertDescription>
                </Alert>
                <div className="flex gap-2">
                  <Input
                    value={newToken}
                    readOnly
                    className="font-mono text-xs"
                  />
                  <Button variant="outline" size="icon" onClick={handleCopy}>
                    {copySuccess ? (
                      <CheckCircle className="h-4 w-4 text-green-500" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                  </Button>
                </div>
                <DialogFooter>
                  <Button onClick={resetDialog}>完成</Button>
                </DialogFooter>
              </div>
            ) : (
              <div className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="name">名称</Label>
                  <Input
                    id="name"
                    placeholder="例如: my-app-token"
                    value={formData.name}
                    onChange={(e) =>
                      setFormData({ ...formData, name: e.target.value })
                    }
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="mode">模式</Label>
                  <Select
                    value={formData.mode}
                    onValueChange={(value: 'web' | 'api' | 'both') =>
                      setFormData({ ...formData, mode: value })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="选择模式" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="both">全部 (Web + API)</SelectItem>
                      <SelectItem value="web">仅 Web</SelectItem>
                      <SelectItem value="api">仅 API</SelectItem>
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">
                    Web 模式使用 claude.ai 会话，API 模式使用 Anthropic API 密钥
                  </p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="expires">有效期</Label>
                  <Select
                    value={formData.expires_in}
                    onValueChange={(value) =>
                      setFormData({ ...formData, expires_in: value })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="选择有效期" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="24h">24 小时</SelectItem>
                      <SelectItem value="168h">7 天</SelectItem>
                      <SelectItem value="720h">30 天</SelectItem>
                      <SelectItem value="2160h">90 天</SelectItem>
                      <SelectItem value="8760h">1 年</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <DialogFooter>
                  <Button
                    variant="outline"
                    onClick={() => setDialogOpen(false)}
                  >
                    取消
                  </Button>
                  <Button
                    onClick={handleGenerate}
                    disabled={!formData.name || generateToken.isPending}
                  >
                    {generateToken.isPending ? '生成中...' : '生成'}
                  </Button>
                </DialogFooter>
              </div>
            )}
          </DialogContent>
        </Dialog>
      </div>

      {/* 使用指南 */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <BookOpen className="h-5 w-5" />
            令牌使用指南
          </CardTitle>
          <CardDescription>
            了解如何使用令牌访问 CCProxy API
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          {/* API 概述 */}
          <div className="space-y-3">
            <h4 className="font-semibold">API 概述</h4>
            <p className="text-sm text-muted-foreground">
              CCProxy 提供 OpenAI 兼容的 API 接口，你可以使用任何支持 OpenAI API 的客户端或库来访问 Claude 模型。
            </p>
            <div className="grid gap-2 md:grid-cols-3">
              <div className="p-3 bg-muted/50 rounded-lg">
                <p className="font-medium text-sm">API 地址</p>
                <code className="text-xs text-muted-foreground">{serverUrl}/v1</code>
              </div>
              <div className="p-3 bg-muted/50 rounded-lg">
                <p className="font-medium text-sm">认证方式</p>
                <code className="text-xs text-muted-foreground">Bearer Token</code>
              </div>
              <div className="p-3 bg-muted/50 rounded-lg">
                <p className="font-medium text-sm">兼容格式</p>
                <code className="text-xs text-muted-foreground">OpenAI API</code>
              </div>
            </div>
          </div>

          {/* 可用模型 */}
          <div className="space-y-3">
            <h4 className="font-semibold">可用模型</h4>
            <div className="grid gap-2 md:grid-cols-2">
              <div className="p-3 border rounded-lg">
                <code className="text-sm font-medium">claude-sonnet-4-20250514</code>
                <p className="text-xs text-muted-foreground mt-1">Claude Sonnet 4 - 平衡性能与速度</p>
              </div>
              <div className="p-3 border rounded-lg">
                <code className="text-sm font-medium">claude-opus-4-20250514</code>
                <p className="text-xs text-muted-foreground mt-1">Claude Opus 4 - 最强大的模型</p>
              </div>
              <div className="p-3 border rounded-lg">
                <code className="text-sm font-medium">claude-3-5-sonnet-20241022</code>
                <p className="text-xs text-muted-foreground mt-1">Claude 3.5 Sonnet</p>
              </div>
              <div className="p-3 border rounded-lg">
                <code className="text-sm font-medium">claude-3-5-haiku-20241022</code>
                <p className="text-xs text-muted-foreground mt-1">Claude 3.5 Haiku - 快速响应</p>
              </div>
            </div>
          </div>

          {/* 代码示例 */}
          <div className="space-y-3">
            <h4 className="font-semibold">代码示例</h4>
            <Tabs defaultValue="curl" className="w-full">
              <TabsList className="grid w-full grid-cols-4">
                <TabsTrigger value="curl" className="flex items-center gap-1">
                  <Terminal className="h-3 w-3" />
                  cURL
                </TabsTrigger>
                <TabsTrigger value="python" className="flex items-center gap-1">
                  <Code className="h-3 w-3" />
                  Python
                </TabsTrigger>
                <TabsTrigger value="node" className="flex items-center gap-1">
                  <Code className="h-3 w-3" />
                  Node.js
                </TabsTrigger>
                <TabsTrigger value="stream" className="flex items-center gap-1">
                  <Terminal className="h-3 w-3" />
                  流式输出
                </TabsTrigger>
              </TabsList>

              <TabsContent value="curl" className="mt-4">
                <div className="relative">
                  <pre className="p-4 bg-muted rounded-lg overflow-x-auto text-xs">
                    <code>{curlExample}</code>
                  </pre>
                  <Button
                    variant="secondary"
                    size="sm"
                    className="absolute top-2 right-2"
                    onClick={() => handleCopyExample(curlExample, 'curl')}
                  >
                    {copiedExample === 'curl' ? (
                      <><CheckCircle className="h-3 w-3 mr-1" />已复制</>
                    ) : (
                      <><Copy className="h-3 w-3 mr-1" />复制</>
                    )}
                  </Button>
                </div>
              </TabsContent>

              <TabsContent value="python" className="mt-4">
                <div className="relative">
                  <pre className="p-4 bg-muted rounded-lg overflow-x-auto text-xs">
                    <code>{pythonExample}</code>
                  </pre>
                  <Button
                    variant="secondary"
                    size="sm"
                    className="absolute top-2 right-2"
                    onClick={() => handleCopyExample(pythonExample, 'python')}
                  >
                    {copiedExample === 'python' ? (
                      <><CheckCircle className="h-3 w-3 mr-1" />已复制</>
                    ) : (
                      <><Copy className="h-3 w-3 mr-1" />复制</>
                    )}
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground mt-2">
                  安装依赖: <code className="bg-muted px-1 rounded">pip install openai</code>
                </p>
              </TabsContent>

              <TabsContent value="node" className="mt-4">
                <div className="relative">
                  <pre className="p-4 bg-muted rounded-lg overflow-x-auto text-xs">
                    <code>{nodeExample}</code>
                  </pre>
                  <Button
                    variant="secondary"
                    size="sm"
                    className="absolute top-2 right-2"
                    onClick={() => handleCopyExample(nodeExample, 'node')}
                  >
                    {copiedExample === 'node' ? (
                      <><CheckCircle className="h-3 w-3 mr-1" />已复制</>
                    ) : (
                      <><Copy className="h-3 w-3 mr-1" />复制</>
                    )}
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground mt-2">
                  安装依赖: <code className="bg-muted px-1 rounded">npm install openai</code>
                </p>
              </TabsContent>

              <TabsContent value="stream" className="mt-4">
                <div className="relative">
                  <pre className="p-4 bg-muted rounded-lg overflow-x-auto text-xs">
                    <code>{streamExample}</code>
                  </pre>
                  <Button
                    variant="secondary"
                    size="sm"
                    className="absolute top-2 right-2"
                    onClick={() => handleCopyExample(streamExample, 'stream')}
                  >
                    {copiedExample === 'stream' ? (
                      <><CheckCircle className="h-3 w-3 mr-1" />已复制</>
                    ) : (
                      <><Copy className="h-3 w-3 mr-1" />复制</>
                    )}
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground mt-2">
                  添加 <code className="bg-muted px-1 rounded">"stream": true</code> 启用流式输出
                </p>
              </TabsContent>
            </Tabs>
          </div>

          {/* 令牌模式说明 */}
          <Alert>
            <Info className="h-4 w-4" />
            <AlertDescription>
              <p className="font-medium mb-2">令牌模式说明</p>
              <ul className="list-disc list-inside space-y-1 text-sm">
                <li><strong>Web 模式:</strong> 使用 claude.ai 会话，需要先在「会话管理」添加会话</li>
                <li><strong>API 模式:</strong> 使用 Anthropic API 密钥，需要配置 CCPROXY_CLAUDE_API_KEYS</li>
                <li><strong>全部模式:</strong> 可同时使用 Web 和 API 模式，系统自动选择可用的方式</li>
              </ul>
            </AlertDescription>
          </Alert>
        </CardContent>
      </Card>

      {/* 令牌列表 */}
      <Card>
        <CardHeader>
          <CardTitle>所有令牌</CardTitle>
          <CardDescription>
            已生成的所有令牌及其状态
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <p className="text-muted-foreground">加载中...</p>
          ) : error ? (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>
                加载令牌失败: {error.message}
              </AlertDescription>
            </Alert>
          ) : data?.tokens.length === 0 ? (
            <p className="text-muted-foreground">
              暂无令牌。点击上方「生成令牌」按钮创建第一个令牌。
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>令牌 ID</TableHead>
                  <TableHead>模式</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>创建时间</TableHead>
                  <TableHead>过期时间</TableHead>
                  <TableHead>最后使用</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data?.tokens.map((token) => (
                  <TableRow key={token.id}>
                    <TableCell className="font-medium">{token.name}</TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">
                      {token.id.substring(0, 8)}...
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">{getModeText(token.mode)}</Badge>
                    </TableCell>
                    <TableCell>
                      {token.revoked_at ? (
                        <Badge variant="destructive">已撤销</Badge>
                      ) : token.is_valid ? (
                        <Badge variant="success">有效</Badge>
                      ) : (
                        <Badge variant="warning">已过期</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {formatDate(token.created_at)}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {formatDate(token.expires_at)}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {token.last_used_at
                        ? formatDate(token.last_used_at)
                        : '从未使用'}
                    </TableCell>
                    <TableCell className="text-right space-x-1">
                      {token.is_valid && !token.revoked_at && (
                        <>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleViewToken(token)}
                            title="查看令牌信息"
                          >
                            <Eye className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleTestToken(token)}
                            title="测试令牌"
                          >
                            <MessageSquare className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleRevoke(token.id)}
                            disabled={revokeToken.isPending}
                            title="撤销"
                          >
                            <Ban className="h-4 w-4" />
                          </Button>
                        </>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* 查看令牌对话框 */}
      <Dialog open={viewTokenDialogOpen} onOpenChange={setViewTokenDialogOpen}>
        <DialogContent className="sm:max-w-[500px]">
          <DialogHeader>
            <DialogTitle>查看令牌信息</DialogTitle>
            <DialogDescription>
              令牌: {viewingToken?.name}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <Alert>
              <Info className="h-4 w-4" />
              <AlertDescription>
                出于安全考虑，系统不保存令牌明文。如需查看完整令牌，请从您保存的位置复制。
              </AlertDescription>
            </Alert>

            <div className="space-y-3">
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <Label className="text-muted-foreground">令牌 ID</Label>
                  <p className="font-mono text-xs mt-1">{viewingToken?.id}</p>
                </div>
                <div>
                  <Label className="text-muted-foreground">模式</Label>
                  <p className="mt-1">{getModeText(viewingToken?.mode)}</p>
                </div>
                <div>
                  <Label className="text-muted-foreground">创建时间</Label>
                  <p className="text-xs mt-1">{viewingToken ? formatDate(viewingToken.created_at) : ''}</p>
                </div>
                <div>
                  <Label className="text-muted-foreground">过期时间</Label>
                  <p className="text-xs mt-1">{viewingToken ? formatDate(viewingToken.expires_at) : ''}</p>
                </div>
              </div>

              <div className="space-y-2">
                <Label>输入完整令牌（可选）</Label>
                <Input
                  type="password"
                  placeholder="粘贴您保存的令牌以验证"
                  value={viewTokenInput}
                  onChange={(e) => setViewTokenInput(e.target.value)}
                  className="font-mono text-xs"
                />
                <p className="text-xs text-muted-foreground">
                  输入令牌后可以验证其是否与此记录匹配
                </p>
              </div>

              {viewTokenInput && (
                <Alert>
                  <CheckCircle className="h-4 w-4" />
                  <AlertDescription>
                    令牌格式有效。您可以在"测试令牌"功能中测试其是否工作正常。
                  </AlertDescription>
                </Alert>
              )}
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setViewTokenDialogOpen(false)}>
              关闭
            </Button>
            {viewTokenInput && (
              <Button onClick={() => {
                setTestTokenInput(viewTokenInput);
                setViewTokenDialogOpen(false);
                handleTestToken(viewingToken);
              }}>
                测试此令牌
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 测试令牌对话框 */}
      <Dialog open={testDialogOpen} onOpenChange={setTestDialogOpen}>
        <DialogContent className="sm:max-w-[500px]">
          <DialogHeader>
            <DialogTitle>测试令牌</DialogTitle>
            <DialogDescription>
              测试令牌: {testingToken?.name}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            {!testResult && !isTesting && (
              <>
                <Alert>
                  <Info className="h-4 w-4" />
                  <AlertDescription>
                    请输入您保存的完整令牌进行测试。系统会发送一条测试消息到 Claude。
                  </AlertDescription>
                </Alert>

                <div className="space-y-2">
                  <Label htmlFor="test-token">令牌</Label>
                  <Input
                    id="test-token"
                    type="password"
                    placeholder="粘贴您的令牌"
                    value={testTokenInput}
                    onChange={(e) => setTestTokenInput(e.target.value)}
                    className="font-mono text-xs"
                  />
                </div>
              </>
            )}

            {isTesting && (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
                <span className="ml-3 text-muted-foreground">正在测试令牌...</span>
              </div>
            )}

            {testResult && (
              <div className="space-y-3">
                <Alert variant={testResult.success ? "default" : "destructive"}>
                  {testResult.success ? (
                    <CheckCircle className="h-4 w-4" />
                  ) : (
                    <AlertCircle className="h-4 w-4" />
                  )}
                  <AlertDescription>{testResult.message}</AlertDescription>
                </Alert>

                {testResult.success && testResult.response && (
                  <div className="space-y-2">
                    <Label>Claude 响应:</Label>
                    <div className="bg-muted p-3 rounded-md text-sm">
                      {testResult.response}
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setTestDialogOpen(false);
                setTestTokenInput('');
                setTestResult(null);
              }}
            >
              关闭
            </Button>
            {!testResult && !isTesting && (
              <Button
                onClick={executeTest}
                disabled={!testTokenInput.trim() || isTesting}
              >
                开始测试
              </Button>
            )}
            {testResult && !testResult.success && (
              <Button
                onClick={executeTest}
                disabled={isTesting}
              >
                重试
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
