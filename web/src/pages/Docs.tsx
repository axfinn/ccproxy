import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Code, Terminal, Settings, CheckCircle2 } from 'lucide-react';

export function Docs() {
  const baseURL = window.location.origin;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">配置文档</h1>
        <p className="text-muted-foreground mt-2">
          如何配置 Claude Code 使用 ccproxy 代理服务
        </p>
      </div>

      <Alert>
        <CheckCircle2 className="h-4 w-4" />
        <AlertDescription>
          本代理已优化请求头，使用最新的浏览器指纹和标准 SDK User-Agent，避免触发风控。
        </AlertDescription>
      </Alert>

      <Tabs defaultValue="setup" className="space-y-4">
        <TabsList>
          <TabsTrigger value="setup">
            <Settings className="h-4 w-4 mr-2" />
            快速开始
          </TabsTrigger>
          <TabsTrigger value="env">
            <Terminal className="h-4 w-4 mr-2" />
            环境变量配置
          </TabsTrigger>
          <TabsTrigger value="advanced">
            <Code className="h-4 w-4 mr-2" />
            高级配置
          </TabsTrigger>
        </TabsList>

        <TabsContent value="setup" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>步骤 1: 生成访问令牌</CardTitle>
              <CardDescription>
                在"令牌管理"页面生成一个新的访问令牌
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <ol className="list-decimal list-inside space-y-2 text-sm">
                <li>访问 <code className="bg-muted px-1 py-0.5 rounded">令牌管理</code> 页面</li>
                <li>点击"生成令牌"按钮</li>
                <li>选择模式：
                  <ul className="list-disc list-inside ml-6 mt-1 space-y-1">
                    <li><code className="bg-muted px-1 py-0.5 rounded">web</code> - 使用 claude.ai 会话（需要配置 Session）</li>
                    <li><code className="bg-muted px-1 py-0.5 rounded">api</code> - 使用 Anthropic API Key</li>
                    <li><code className="bg-muted px-1 py-0.5 rounded">both</code> - 两种模式都可用（默认 API 优先）</li>
                  </ul>
                </li>
                <li>复制生成的令牌（只显示一次，请妥善保管）</li>
              </ol>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>步骤 2: 配置 Claude Code</CardTitle>
              <CardDescription>
                配置 Claude Code 使用代理服务
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <h4 className="font-semibold mb-2">方法一: 使用环境变量（推荐）</h4>
                <p className="text-sm text-muted-foreground mb-3">
                  在 shell 配置文件（~/.bashrc, ~/.zshrc）中添加：
                </p>
                <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-sm">
                  <code>{`# ccproxy 配置
export ANTHROPIC_BASE_URL="${baseURL}/v1"
export ANTHROPIC_API_KEY="your_token_here"

# 或者使用 Claude Code 环境变量
export CLAUDE_API_BASE_URL="${baseURL}/v1"
export CLAUDE_API_KEY="your_token_here"`}</code>
                </pre>
                <p className="text-sm text-muted-foreground mt-2">
                  保存后运行 <code className="bg-muted px-1 py-0.5 rounded">source ~/.zshrc</code> 或重启终端
                </p>
              </div>

              <div>
                <h4 className="font-semibold mb-2">方法二: 使用配置文件</h4>
                <p className="text-sm text-muted-foreground mb-3">
                  创建或编辑 <code className="bg-muted px-1 py-0.5 rounded">~/.config/claude/config.json</code>：
                </p>
                <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-sm">
                  <code>{`{
  "api": {
    "baseUrl": "${baseURL}/v1",
    "key": "your_token_here"
  }
}`}</code>
                </pre>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>步骤 3: 验证配置</CardTitle>
              <CardDescription>
                测试代理是否正常工作
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <h4 className="font-semibold mb-2">使用测试脚本</h4>
                <p className="text-sm text-muted-foreground mb-3">
                  运行以下命令测试连接：
                </p>
                <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-sm">
                  <code>{`# 使用项目提供的测试脚本
./test-proxy.sh your_token_here

# 或手动测试
curl -X POST ${baseURL}/v1/chat/completions \\
  -H "Authorization: Bearer your_token_here" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 100,
    "stream": false
  }'`}</code>
                </pre>
              </div>

              <div>
                <h4 className="font-semibold mb-2">使用 Claude Code CLI</h4>
                <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-sm">
                  <code>{`# 测试 Claude Code
claude "Hello, test connection"

# 查看模型列表
curl ${baseURL}/v1/models \\
  -H "Authorization: Bearer your_token_here"`}</code>
                </pre>
              </div>

              <Alert className="mt-4">
                <CheckCircle2 className="h-4 w-4" />
                <AlertDescription>
                  如果配置正确，Claude Code 将通过代理访问 Claude API，所有请求都会经过 ccproxy 转发。
                </AlertDescription>
              </Alert>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="env" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>环境变量参考</CardTitle>
              <CardDescription>
                Claude Code 支持的环境变量
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                <div>
                  <h4 className="font-semibold text-sm">ANTHROPIC_BASE_URL</h4>
                  <p className="text-sm text-muted-foreground mb-2">
                    Anthropic API 的基础 URL（不包含 /v1）
                  </p>
                  <pre className="bg-muted p-3 rounded text-sm">
                    <code>export ANTHROPIC_BASE_URL="{baseURL}/v1"</code>
                  </pre>
                </div>

                <div>
                  <h4 className="font-semibold text-sm">ANTHROPIC_API_KEY</h4>
                  <p className="text-sm text-muted-foreground mb-2">
                    访问令牌（从令牌管理页面生成）
                  </p>
                  <pre className="bg-muted p-3 rounded text-sm">
                    <code>export ANTHROPIC_API_KEY="your_token_here"</code>
                  </pre>
                </div>

                <div>
                  <h4 className="font-semibold text-sm">CLAUDE_API_BASE_URL / CLAUDE_API_KEY</h4>
                  <p className="text-sm text-muted-foreground mb-2">
                    Claude Code 特定的环境变量（优先级高于 ANTHROPIC_*）
                  </p>
                  <pre className="bg-muted p-3 rounded text-sm">
                    <code>{`export CLAUDE_API_BASE_URL="${baseURL}/v1"
export CLAUDE_API_KEY="your_token_here"`}</code>
                  </pre>
                </div>

                <Alert>
                  <AlertDescription>
                    <strong>注意：</strong>BASE_URL 应该包含 /v1 后缀，因为 Claude Code 会直接拼接 /messages 等路径。
                  </AlertDescription>
                </Alert>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>模式选择</CardTitle>
              <CardDescription>
                通过请求头控制使用 Web 模式或 API 模式
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                <p className="text-sm text-muted-foreground">
                  当令牌模式为 <code className="bg-muted px-1 py-0.5 rounded">both</code> 时，可以通过 <code className="bg-muted px-1 py-0.5 rounded">X-Proxy-Mode</code> 请求头指定模式：
                </p>

                <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-sm">
                  <code>{`# 强制使用 Web 模式（claude.ai）
curl -X POST ${baseURL}/v1/chat/completions \\
  -H "Authorization: Bearer your_token" \\
  -H "X-Proxy-Mode: web" \\
  -H "Content-Type: application/json" \\
  -d '{"model": "claude-3-5-sonnet-20241022", ...}'

# 强制使用 API 模式（Anthropic API）
curl -X POST ${baseURL}/v1/chat/completions \\
  -H "Authorization: Bearer your_token" \\
  -H "X-Proxy-Mode: api" \\
  -H "Content-Type: application/json" \\
  -d '{"model": "claude-3-5-sonnet-20241022", ...}'`}</code>
                </pre>

                <div className="text-sm space-y-2 mt-4">
                  <p><strong>默认行为（不指定 X-Proxy-Mode）：</strong></p>
                  <ul className="list-disc list-inside space-y-1 ml-4">
                    <li>令牌模式为 <code className="bg-muted px-1 py-0.5 rounded">web</code> → 使用 Web 模式</li>
                    <li>令牌模式为 <code className="bg-muted px-1 py-0.5 rounded">api</code> → 使用 API 模式</li>
                    <li>令牌模式为 <code className="bg-muted px-1 py-0.5 rounded">both</code> → 优先使用 API 模式（如果有可用 API Key）</li>
                  </ul>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="advanced" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>风控优化说明</CardTitle>
              <CardDescription>
                ccproxy 的反风控措施
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4 text-sm">
                <div>
                  <h4 className="font-semibold mb-2">Web 模式优化</h4>
                  <ul className="list-disc list-inside space-y-1 ml-4">
                    <li>使用最新的 Chrome 131 User-Agent</li>
                    <li>添加现代浏览器的 Client Hints (Sec-Ch-Ua-*)</li>
                    <li>完整的 Sec-Fetch-* 安全头</li>
                    <li>正确的 Accept-Encoding (gzip, deflate, br, zstd)</li>
                    <li>完整的 Accept-Language 语言列表</li>
                    <li>正确的 Origin 和 Referer 头</li>
                  </ul>
                </div>

                <div>
                  <h4 className="font-semibold mb-2">API 模式优化</h4>
                  <ul className="list-disc list-inside space-y-1 ml-4">
                    <li>使用标准 SDK User-Agent (anthropic-sdk-go)</li>
                    <li>添加 Accept-Encoding 支持压缩</li>
                    <li>完整的 Anthropic API 头 (anthropic-version)</li>
                    <li>自动 API Key 轮询和健康检查</li>
                  </ul>
                </div>

                <Alert>
                  <AlertDescription>
                    这些优化使代理的请求看起来像真实的浏览器或官方 SDK，大大降低被风控的概率。
                  </AlertDescription>
                </Alert>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>流式响应</CardTitle>
              <CardDescription>
                支持 Server-Sent Events (SSE) 流式响应
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground mb-3">
                ccproxy 完全支持流式响应，自动转换为 OpenAI 格式：
              </p>
              <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-sm">
                <code>{`curl -X POST ${baseURL}/v1/chat/completions \\
  -H "Authorization: Bearer your_token" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "messages": [{"role": "user", "content": "Write a poem"}],
    "max_tokens": 1024,
    "stream": true
  }'`}</code>
              </pre>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>故障排查</CardTitle>
              <CardDescription>
                常见问题和解决方案
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4 text-sm">
                <div>
                  <h4 className="font-semibold">问题：401 Unauthorized</h4>
                  <p className="text-muted-foreground">
                    检查令牌是否正确，是否已过期或被撤销
                  </p>
                </div>

                <div>
                  <h4 className="font-semibold">问题：503 Service Unavailable</h4>
                  <p className="text-muted-foreground">
                    Web 模式：检查是否配置了活跃的 Session<br />
                    API 模式：检查是否配置了有效的 API Key
                  </p>
                </div>

                <div>
                  <h4 className="font-semibold">问题：仍然触发风控</h4>
                  <p className="text-muted-foreground">
                    1. 确保使用的是最新版本的 ccproxy<br />
                    2. 检查 Session/API Key 是否有效<br />
                    3. 避免短时间内大量请求<br />
                    4. 如果使用 Web 模式，建议配置多个 Session 轮询
                  </p>
                </div>

                <div>
                  <h4 className="font-semibold">问题：Claude Code 连接失败</h4>
                  <p className="text-muted-foreground">
                    1. 检查环境变量是否正确设置（BASE_URL 包含 /v1）<br />
                    2. 确认代理服务正在运行<br />
                    3. 测试网络连接：<code className="bg-muted px-1 py-0.5 rounded">curl {baseURL}/v1/models -H "Authorization: Bearer your_token"</code>
                  </p>
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
