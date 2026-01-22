import { useState } from 'react';
import {
  useSessions,
  useAddSession,
  useDeleteSession,
  useDeactivateSession,
} from '@/hooks/useSessions';
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
import { Plus, Trash2, Power, AlertCircle, BookMarked, Copy, CheckCircle, Info, MessageSquare, Loader2 } from 'lucide-react';

const BOOKMARKLET_CODE = `javascript:(function(){
  var sk = null;
  var org = '';

  /* 尝试多种方式获取 sessionKey */
  var cookies = document.cookie.split(';');
  for(var i=0; i<cookies.length; i++){
    var c = cookies[i].trim();
    if(c.startsWith('sessionKey=')){
      sk = c.substring(11);
      break;
    }
    if(c.startsWith('__Secure-next-auth.session-token=')){
      sk = c.substring(33);
      break;
    }
    if(c.startsWith('lastActiveOrg=')){
      org = c.substring(14);
    }
  }

  /* 尝试从 localStorage 获取 */
  if(!sk){
    try{
      sk = localStorage.getItem('sessionKey');
    }catch(e){}
  }

  /* 尝试从 __NEXT_DATA__ 获取组织ID */
  if(!org){
    try{
      org = window.__NEXT_DATA__?.props?.pageProps?.initialActiveOrg?.uuid || '';
    }catch(e){}
  }

  if(sk){
    var data = {sessionKey: sk, organizationId: org};
    prompt('会话数据 (请复制):', JSON.stringify(data));
  }else{
    alert('未找到会话。\\n\\n可能的原因：\\n1. 请确保已登录 claude.ai\\n2. 请在 claude.ai 主页面使用此书签\\n3. 如果仍然失败，请手动从浏览器开发者工具获取 Cookie');
  }
})();`;

export function Sessions() {
  const { data, isLoading, error } = useSessions();
  const addSession = useAddSession();
  const deleteSession = useDeleteSession();
  const deactivateSession = useDeactivateSession();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [formData, setFormData] = useState({
    name: '',
    session_key: '',
    organization_id: '',
  });
  const [copySuccess, setCopySuccess] = useState(false);
  const [testDialogOpen, setTestDialogOpen] = useState(false);
  const [testingSession, setTestingSession] = useState<any>(null);
  const [testResult, setTestResult] = useState<{ success: boolean; message: string; response?: string } | null>(null);
  const [isTesting, setIsTesting] = useState(false);

  const handleAdd = async () => {
    try {
      await addSession.mutateAsync({
        name: formData.name,
        session_key: formData.session_key,
        organization_id: formData.organization_id || undefined,
      });
      setFormData({ name: '', session_key: '', organization_id: '' });
      setDialogOpen(false);
    } catch (err) {
      console.error('添加会话失败:', err);
    }
  };

  const handleDelete = async (id: string) => {
    if (confirm('确定要删除此会话吗？')) {
      try {
        await deleteSession.mutateAsync(id);
      } catch (err) {
        console.error('删除会话失败:', err);
      }
    }
  };

  const handleDeactivate = async (id: string) => {
    if (confirm('确定要停用此会话吗？')) {
      try {
        await deactivateSession.mutateAsync(id);
      } catch (err) {
        console.error('停用会话失败:', err);
      }
    }
  };

  const copyBookmarklet = async () => {
    await navigator.clipboard.writeText(BOOKMARKLET_CODE);
    setCopySuccess(true);
    setTimeout(() => setCopySuccess(false), 2000);
  };

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleString('zh-CN');
  };

  const parseSessionData = (jsonStr: string) => {
    try {
      const data = JSON.parse(jsonStr);
      if (data.sessionKey) {
        setFormData({
          ...formData,
          session_key: data.sessionKey,
          organization_id: data.organizationId || '',
        });
      }
    } catch {
      // 不是 JSON，假设是 session key
      setFormData({ ...formData, session_key: jsonStr });
    }
  };

  const handleTestSession = async (session: any) => {
    setTestingSession(session);
    setTestResult(null);
    setTestDialogOpen(true);
    setIsTesting(true);

    try {
      // 使用 OpenAI 格式的 API 测试 Session
      const token = localStorage.getItem('authToken');
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`,
          'X-Proxy-Mode': 'web',
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
          message: '会话测试成功！Session 工作正常。',
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

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">会话管理</h1>
          <p className="text-muted-foreground">
            管理 Web 模式的 claude.ai 会话
          </p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              添加会话
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-[600px]">
            <DialogHeader>
              <DialogTitle>添加新会话</DialogTitle>
              <DialogDescription>
                添加 claude.ai 会话以启用 Web 模式
              </DialogDescription>
            </DialogHeader>

            <Tabs defaultValue="manual" className="w-full">
              <TabsList className="grid w-full grid-cols-2">
                <TabsTrigger value="manual">手动输入</TabsTrigger>
                <TabsTrigger value="bookmarklet">书签脚本</TabsTrigger>
              </TabsList>

              <TabsContent value="manual" className="space-y-4 mt-4">
                <div className="space-y-2">
                  <Label htmlFor="name">会话名称</Label>
                  <Input
                    id="name"
                    placeholder="例如: 我的账号"
                    value={formData.name}
                    onChange={(e) =>
                      setFormData({ ...formData, name: e.target.value })
                    }
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="session_key">Session Key</Label>
                  <Textarea
                    id="session_key"
                    placeholder="粘贴 session key 或书签脚本的 JSON 数据"
                    value={formData.session_key}
                    onChange={(e) => parseSessionData(e.target.value)}
                    rows={3}
                  />
                  <p className="text-xs text-muted-foreground">
                    claude.ai 的 sessionKey Cookie
                  </p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="org_id">组织 ID (可选)</Label>
                  <Input
                    id="org_id"
                    placeholder="例如: uuid-of-organization"
                    value={formData.organization_id}
                    onChange={(e) =>
                      setFormData({ ...formData, organization_id: e.target.value })
                    }
                  />
                </div>
              </TabsContent>

              <TabsContent value="bookmarklet" className="space-y-4 mt-4">
                <Alert>
                  <BookMarked className="h-4 w-4" />
                  <AlertTitle>会话提取书签脚本</AlertTitle>
                  <AlertDescription className="mt-2">
                    <ol className="list-decimal list-inside space-y-2 text-sm">
                      <li>复制下方的书签脚本代码</li>
                      <li>在浏览器中创建新书签</li>
                      <li>将代码粘贴为书签的 URL</li>
                      <li>访问 claude.ai 并登录</li>
                      <li>点击书签提取会话数据</li>
                      <li>将结果粘贴到"手动输入"标签页</li>
                    </ol>
                  </AlertDescription>
                </Alert>

                <div className="space-y-2">
                  <Label>书签脚本代码</Label>
                  <div className="relative">
                    <Textarea
                      value={BOOKMARKLET_CODE}
                      readOnly
                      rows={6}
                      className="font-mono text-xs pr-12"
                    />
                    <Button
                      variant="ghost"
                      size="icon"
                      className="absolute top-2 right-2"
                      onClick={copyBookmarklet}
                    >
                      {copySuccess ? (
                        <CheckCircle className="h-4 w-4 text-green-500" />
                      ) : (
                        <Copy className="h-4 w-4" />
                      )}
                    </Button>
                  </div>
                </div>
              </TabsContent>
            </Tabs>

            <DialogFooter>
              <Button variant="outline" onClick={() => setDialogOpen(false)}>
                取消
              </Button>
              <Button
                onClick={handleAdd}
                disabled={!formData.name || !formData.session_key || addSession.isPending}
              >
                {addSession.isPending ? '添加中...' : '添加会话'}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {/* 书签脚本使用指南 */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <BookMarked className="h-5 w-5" />
            书签脚本使用指南
          </CardTitle>
          <CardDescription>
            通过书签脚本快速提取 claude.ai 会话信息
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          {/* 书签代码 */}
          <div className="space-y-2">
            <Label className="text-base font-semibold">书签脚本代码</Label>
            <div className="relative">
              <Textarea
                value={BOOKMARKLET_CODE}
                readOnly
                rows={6}
                className="font-mono text-xs pr-12 bg-muted"
              />
              <Button
                variant="secondary"
                size="sm"
                className="absolute top-2 right-2"
                onClick={copyBookmarklet}
              >
                {copySuccess ? (
                  <>
                    <CheckCircle className="h-4 w-4 mr-1 text-green-500" />
                    已复制
                  </>
                ) : (
                  <>
                    <Copy className="h-4 w-4 mr-1" />
                    复制代码
                  </>
                )}
              </Button>
            </div>
          </div>

          {/* 使用步骤 */}
          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-3">
              <h4 className="font-semibold text-sm flex items-center gap-2">
                <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary text-primary-foreground text-xs">1</span>
                创建书签
              </h4>
              <div className="text-sm text-muted-foreground pl-8 space-y-1">
                <p><strong>Chrome/Edge:</strong></p>
                <ol className="list-decimal list-inside space-y-1 ml-2">
                  <li>右键点击书签栏 → 添加网页</li>
                  <li>名称填写: <code className="bg-muted px-1 rounded">提取Claude会话</code></li>
                  <li>网址粘贴上方的书签脚本代码</li>
                  <li>点击保存</li>
                </ol>
              </div>
            </div>

            <div className="space-y-3">
              <h4 className="font-semibold text-sm flex items-center gap-2">
                <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary text-primary-foreground text-xs">2</span>
                登录 Claude.ai
              </h4>
              <div className="text-sm text-muted-foreground pl-8 space-y-1">
                <ol className="list-decimal list-inside space-y-1">
                  <li>打开浏览器访问 <a href="https://claude.ai" target="_blank" rel="noopener noreferrer" className="text-primary hover:underline">claude.ai</a></li>
                  <li>使用你的账号登录</li>
                  <li>确保能看到聊天界面</li>
                </ol>
              </div>
            </div>

            <div className="space-y-3">
              <h4 className="font-semibold text-sm flex items-center gap-2">
                <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary text-primary-foreground text-xs">3</span>
                提取会话数据
              </h4>
              <div className="text-sm text-muted-foreground pl-8 space-y-1">
                <ol className="list-decimal list-inside space-y-1">
                  <li>在 claude.ai 页面点击书签</li>
                  <li>弹出对话框显示会话数据</li>
                  <li>全选复制对话框内容 (Ctrl+A, Ctrl+C)</li>
                </ol>
              </div>
            </div>

            <div className="space-y-3">
              <h4 className="font-semibold text-sm flex items-center gap-2">
                <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary text-primary-foreground text-xs">4</span>
                添加到 CCProxy
              </h4>
              <div className="text-sm text-muted-foreground pl-8 space-y-1">
                <ol className="list-decimal list-inside space-y-1">
                  <li>点击上方「添加会话」按钮</li>
                  <li>填写会话名称</li>
                  <li>粘贴复制的 JSON 数据</li>
                  <li>点击添加会话</li>
                </ol>
              </div>
            </div>
          </div>

          {/* 手动获取方法 */}
          <div className="space-y-3 p-4 bg-muted/50 rounded-lg">
            <h4 className="font-semibold text-sm flex items-center gap-2">
              <AlertCircle className="h-4 w-4" />
              书签脚本无法使用？手动获取 Cookie
            </h4>
            <div className="text-sm text-muted-foreground space-y-2">
              <p>如果书签脚本提示"未找到会话"，可以手动从浏览器获取：</p>
              <ol className="list-decimal list-inside space-y-1 ml-2">
                <li>在 claude.ai 页面按 <kbd className="px-1.5 py-0.5 bg-background border rounded text-xs">F12</kbd> 打开开发者工具</li>
                <li>切换到 <strong>Application</strong> (应用) 标签页</li>
                <li>在左侧找到 <strong>Cookies</strong> → <strong>https://claude.ai</strong></li>
                <li>找到名为 <code className="bg-background px-1 rounded">sessionKey</code> 的 Cookie</li>
                <li>双击 Value 列复制值，粘贴到上方的"手动输入"</li>
              </ol>
              <p className="text-xs mt-2">
                <strong>提示：</strong>如果找不到 sessionKey，也可以尝试查找 <code className="bg-background px-1 rounded">__Secure-next-auth.session-token</code>
              </p>
            </div>
          </div>

          {/* 注意事项 */}
          <Alert>
            <Info className="h-4 w-4" />
            <AlertTitle>注意事项</AlertTitle>
            <AlertDescription>
              <ul className="list-disc list-inside space-y-1 mt-2 text-sm">
                <li><strong>会话有效期:</strong> 会话通常有效期较长，但在网页退出登录后会失效</li>
                <li><strong>安全提醒:</strong> Session Key 相当于登录凭证，请勿泄露给他人</li>
                <li><strong>多账号支持:</strong> 可添加多个账号的会话，系统会轮询使用</li>
                <li><strong>组织 ID:</strong> Claude Pro/Team 用户的组织 ID 会自动提取</li>
              </ul>
            </AlertDescription>
          </Alert>
        </CardContent>
      </Card>

      {/* 会话列表 */}
      <Card>
        <CardHeader>
          <CardTitle>所有会话</CardTitle>
          <CardDescription>
            用于 Web 模式的 claude.ai 会话
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <p className="text-muted-foreground">加载中...</p>
          ) : error ? (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>
                加载会话失败: {error.message}
              </AlertDescription>
            </Alert>
          ) : data?.sessions.length === 0 ? (
            <p className="text-muted-foreground">
              暂无会话。按照上方指南添加会话以启用 Web 模式。
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>组织 ID</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>创建时间</TableHead>
                  <TableHead>过期时间</TableHead>
                  <TableHead>最后使用</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data?.sessions.map((session) => (
                  <TableRow key={session.id}>
                    <TableCell className="font-medium">{session.name}</TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {session.organization_id || '-'}
                    </TableCell>
                    <TableCell>
                      {session.is_active ? (
                        <Badge variant="success">活跃</Badge>
                      ) : (
                        <Badge variant="secondary">已停用</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {formatDate(session.created_at)}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {session.expires_at
                        ? formatDate(session.expires_at)
                        : '永不过期'}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {session.last_used_at
                        ? formatDate(session.last_used_at)
                        : '从未使用'}
                    </TableCell>
                    <TableCell className="text-right space-x-1">
                      {session.is_active && (
                        <>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleTestSession(session)}
                            title="测试会话"
                          >
                            <MessageSquare className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleDeactivate(session.id)}
                            disabled={deactivateSession.isPending}
                            title="停用"
                          >
                            <Power className="h-4 w-4" />
                          </Button>
                        </>
                      )}
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleDelete(session.id)}
                        disabled={deleteSession.isPending}
                        title="删除"
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* 测试对话框 */}
      <Dialog open={testDialogOpen} onOpenChange={setTestDialogOpen}>
        <DialogContent className="sm:max-w-[500px]">
          <DialogHeader>
            <DialogTitle>测试会话</DialogTitle>
            <DialogDescription>
              测试会话: {testingSession?.name}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            {isTesting ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
                <span className="ml-3 text-muted-foreground">正在测试会话...</span>
              </div>
            ) : testResult ? (
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
            ) : null}
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setTestDialogOpen(false)}
            >
              关闭
            </Button>
            {testResult && !testResult.success && (
              <Button
                onClick={() => handleTestSession(testingSession)}
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
