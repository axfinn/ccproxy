import { useApiKeys } from '@/hooks/useApiKeys';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { AlertCircle, Server, CheckCircle, XCircle, Info } from 'lucide-react';

export function ApiKeys() {
  const { data, isLoading, error } = useApiKeys();

  const maskKey = (key: string) => {
    if (key.length <= 12) return '****';
    return `${key.slice(0, 8)}...${key.slice(-4)}`;
  };

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleString('zh-CN');
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">API 密钥</h1>
        <p className="text-muted-foreground">
          Anthropic API 密钥池统计
        </p>
      </div>

      <Alert>
        <Info className="h-4 w-4" />
        <AlertTitle>API 密钥配置</AlertTitle>
        <AlertDescription>
          API 密钥通过 <code className="bg-muted px-1 rounded">CCPROXY_CLAUDE_API_KEYS</code> 环境变量配置。
          更新密钥后需要重启服务。
        </AlertDescription>
      </Alert>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">总密钥数</CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="text-2xl font-bold">...</div>
            ) : (
              <div className="text-2xl font-bold">{data?.total_keys ?? 0}</div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">健康密钥</CardTitle>
            <CheckCircle className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="text-2xl font-bold">...</div>
            ) : (
              <div className="text-2xl font-bold text-green-600">
                {data?.healthy_keys ?? 0}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">异常密钥</CardTitle>
            <XCircle className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="text-2xl font-bold">...</div>
            ) : (
              <div className="text-2xl font-bold text-red-600">
                {(data?.total_keys ?? 0) - (data?.healthy_keys ?? 0)}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>密钥统计</CardTitle>
          <CardDescription>
            每个 API 密钥的使用统计
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <p className="text-muted-foreground">加载中...</p>
          ) : error ? (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>
                加载 API 密钥统计失败: {error.message}
              </AlertDescription>
            </Alert>
          ) : !data?.keys || data.keys.length === 0 ? (
            <p className="text-muted-foreground">
              未配置 API 密钥。请设置 CCPROXY_CLAUDE_API_KEYS 环境变量。
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>密钥</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className="text-right">请求数</TableHead>
                  <TableHead className="text-right">错误数</TableHead>
                  <TableHead>最后使用</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.keys.map((key, index) => (
                  <TableRow key={index}>
                    <TableCell className="font-mono text-sm">
                      {maskKey(key.key)}
                    </TableCell>
                    <TableCell>
                      {key.is_healthy ? (
                        <Badge variant="success">健康</Badge>
                      ) : (
                        <Badge variant="destructive">异常</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right">{key.requests}</TableCell>
                    <TableCell className="text-right">
                      {key.errors > 0 ? (
                        <span className="text-red-600">{key.errors}</span>
                      ) : (
                        key.errors
                      )}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {key.last_used ? formatDate(key.last_used) : '从未使用'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
