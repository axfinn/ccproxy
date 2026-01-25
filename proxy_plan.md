 # ccproxy 用量统计、使用记录和对话内容记录系统实现计划                                                
                                                                                                        
  ## 目标概述                                                                                           
                                                                                                        
  为 ccproxy 添加完整的用量统计、请求日志记录和对话内容记录功能，支持：                                 
  1. Token 和 Account 级别的详细用量统计                                                                
  2. 每次请求的完整日志记录（请求时间、响应时间、token 使用量、错误信息等）                             
  3. 可选的对话内容记录（用户输入、模型输出、系统提示词、对话历史）                                     
  4. 在 Token 级别支持手动脱敏选项（选择是否记录对话内容）                                              
  5. 永久保留所有记录（需要压缩和归档策略）                                                             
  6. 前端可视化界面展示统计和日志                                                                       
                                                                                                        
  ## 核心设计原则                                                                                       
                                                                                                        
  - **异步日志记录**：使用 goroutine + 缓冲 channel，避免阻塞主请求路径                                 
  - **渐进式实现**：分阶段实现，先核心日志，后统计聚合，最后对话记录                                    
  - **性能优先**：日志写入不能影响请求性能，使用批量写入和索引优化                                      
  - **隐私保护**：对话记录默认禁用，需要在 Token 级别显式启用                                           
                                                                                                        
  ---                                                                                                   
                                                                                                        
  ## 第一阶段：数据库模式和核心日志 (Priority 1)                                                        
                                                                                                        
  ### 1.1 数据库表设计                                                                                  
                                                                                                        
  #### request_logs 表（详细请求日志）                                                                  
  ```sql                                                                                                
  CREATE TABLE request_logs (                                                                           
  id TEXT PRIMARY KEY,                                                                                  
  token_id TEXT NOT NULL,                                                                               
  account_id TEXT,                                                                                      
  user_name TEXT,                                                                                       
  mode TEXT NOT NULL,              -- 'web' or 'api'                                                    
  model TEXT NOT NULL,                                                                                  
  stream BOOLEAN NOT NULL,                                                                              
  request_at DATETIME NOT NULL,                                                                         
  response_at DATETIME,                                                                                 
  duration_ms INTEGER,                                                                                  
  ttft_ms INTEGER,                                                                                      
  prompt_tokens INTEGER DEFAULT 0,                                                                      
  completion_tokens INTEGER DEFAULT 0,                                                                  
  total_tokens INTEGER DEFAULT 0,                                                                       
  status_code INTEGER NOT NULL,                                                                         
  success BOOLEAN NOT NULL,                                                                             
  error_message TEXT,                                                                                   
  conversation_id TEXT,                                                                                 
  FOREIGN KEY (token_id) REFERENCES tokens(id) ON DELETE CASCADE,                                       
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE SET NULL                                   
  );                                                                                                    
                                                                                                        
  CREATE INDEX idx_request_logs_token_id ON request_logs(token_id, request_at DESC);                    
  CREATE INDEX idx_request_logs_account_id ON request_logs(account_id, request_at DESC);                
  CREATE INDEX idx_request_logs_request_at ON request_logs(request_at DESC);                            
  CREATE INDEX idx_request_logs_status ON request_logs(success, status_code);                           
  ```                                                                                                   
                                                                                                        
  #### conversation_contents 表（对话内容）                                                             
  ```sql                                                                                                
  CREATE TABLE conversation_contents (                                                                  
  id TEXT PRIMARY KEY,                                                                                  
  request_log_id TEXT NOT NULL,                                                                         
  token_id TEXT NOT NULL,                                                                               
  system_prompt TEXT,                                                                                   
  messages_json TEXT NOT NULL,     -- JSON 数组的对话历史                                               
  prompt TEXT NOT NULL,            -- 用户当前输入                                                      
  completion TEXT NOT NULL,        -- 模型输出                                                          
  created_at DATETIME NOT NULL,                                                                         
  is_compressed BOOLEAN DEFAULT 0,                                                                      
  FOREIGN KEY (request_log_id) REFERENCES request_logs(id) ON DELETE CASCADE,                           
  FOREIGN KEY (token_id) REFERENCES tokens(id) ON DELETE CASCADE                                        
  );                                                                                                    
                                                                                                        
  CREATE INDEX idx_conversation_token_id ON conversation_contents(token_id, created_at DESC);           
                                                                                                        
  -- 全文搜索索引                                                                                       
  CREATE VIRTUAL TABLE conversation_search USING fts5(                                                  
  id UNINDEXED,                                                                                         
  prompt,                                                                                               
  completion,                                                                                           
  content='conversation_contents',                                                                      
  content_rowid='rowid'                                                                                 
  );                                                                                                    
  ```                                                                                                   
                                                                                                        
  #### tokens 表扩展                                                                                    
  ```sql                                                                                                
  ALTER TABLE tokens ADD COLUMN enable_conversation_logging BOOLEAN DEFAULT 0;                          
  ALTER TABLE tokens ADD COLUMN total_requests INTEGER DEFAULT 0;                                       
  ALTER TABLE tokens ADD COLUMN total_tokens_used INTEGER DEFAULT 0;                                    
  ```                                                                                                   
                                                                                                        
  #### 文件位置                                                                                         
  - `internal/store/sqlite.go` - 添加迁移逻辑到 `migrate()` 方法                                        
                                                                                                        
  ### 1.2 Store 层实现                                                                                  
                                                                                                        
  #### internal/store/request_log.go（新文件）                                                          
  ```go                                                                                                 
  type RequestLog struct {                                                                              
  ID               string                                                                               
  TokenID          string                                                                               
  AccountID        sql.NullString                                                                       
  UserName         string                                                                               
  Mode             string                                                                               
  Model            string                                                                               
  Stream           bool                                                                                 
  RequestAt        time.Time                                                                            
  ResponseAt       sql.NullTime                                                                         
  DurationMs       sql.NullInt64                                                                        
  TTFTMs           sql.NullInt64                                                                        
  PromptTokens     int                                                                                  
  CompletionTokens int                                                                                  
  TotalTokens      int                                                                                  
  StatusCode       int                                                                                  
  Success          bool                                                                                 
  ErrorMessage     sql.NullString                                                                       
  ConversationID   sql.NullString                                                                       
  }                                                                                                     
                                                                                                        
  // 核心方法                                                                                           
  func (s *Store) CreateRequestLog(log *RequestLog) error                                               
  func (s *Store) GetRequestLog(id string) (*RequestLog, error)                                         
  func (s *Store) ListRequestLogs(filter RequestLogFilter) ([]*RequestLog, int, error)                  
  ```                                                                                                   
                                                                                                        
  **RequestLogFilter 结构**:                                                                            
  ```go                                                                                                 
  type RequestLogFilter struct {                                                                        
  TokenID    string                                                                                     
  AccountID  string                                                                                     
  UserName   string                                                                                     
  Mode       string                                                                                     
  Model      string                                                                                     
  Success    *bool                                                                                      
  FromDate   *time.Time                                                                                 
  ToDate     *time.Time                                                                                 
  Page       int                                                                                        
  Limit      int                                                                                        
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  #### internal/store/conversation.go（新文件）                                                         
  ```go                                                                                                 
  type ConversationContent struct {                                                                     
  ID            string                                                                                  
  RequestLogID  string                                                                                  
  TokenID       string                                                                                  
  SystemPrompt  sql.NullString                                                                          
  MessagesJSON  string  // JSON encoded []Message                                                       
  Prompt        string                                                                                  
  Completion    string                                                                                  
  CreatedAt     time.Time                                                                               
  IsCompressed  bool                                                                                    
  }                                                                                                     
                                                                                                        
  // 核心方法                                                                                           
  func (s *Store) CreateConversation(conv *ConversationContent) error                                   
  func (s *Store) GetConversation(id string) (*ConversationContent, error)                              
  func (s *Store) ListConversations(tokenID string, filter ConversationFilter)                          
  ([]*ConversationContent, int, error)                                                                  
  func (s *Store) SearchConversations(tokenID string, query string, limit int)                          
  ([]*ConversationContent, error)                                                                       
  ```                                                                                                   
                                                                                                        
  ### 1.3 异步日志服务                                                                                  
                                                                                                        
  #### internal/service/request_logger.go（新文件）                                                     
  ```go                                                                                                 
  type RequestLogger struct {                                                                           
  store   *store.Store                                                                                  
  queue   chan *LogEntry                                                                                
  workers int                                                                                           
  wg      sync.WaitGroup                                                                                
  ctx     context.Context                                                                               
  cancel  context.CancelFunc                                                                            
  }                                                                                                     
                                                                                                        
  type LogEntry struct {                                                                                
  Log          *store.RequestLog                                                                        
  Conversation *store.ConversationContent                                                               
  }                                                                                                     
                                                                                                        
  // 关键实现                                                                                           
  func NewRequestLogger(store *store.Store, bufferSize, workers int) *RequestLogger                     
  func (rl *RequestLogger) Start(ctx context.Context) error                                             
  func (rl *RequestLogger) Stop() error                                                                 
  func (rl *RequestLogger) LogRequest(entry *LogEntry) error  // 非阻塞                                 
  func (rl *RequestLogger) processQueue(ctx context.Context)  // Worker goroutine                       
  ```                                                                                                   
                                                                                                        
  **设计要点**：                                                                                        
  - 缓冲 channel 大小：10000（可配置）                                                                  
  - Worker 数量：4（可配置）                                                                            
  - 批量写入：每次最多批量插入 100 条记录                                                               
  - 背压处理：channel 满时记录警告，丢弃最旧的条目                                                      
  - 优雅关闭：context cancel 后刷新所有待处理条目                                                       
                                                                                                        
  ### 1.4 集成到请求处理器                                                                              
                                                                                                        
  #### internal/handler/enhanced_proxy.go（修改）                                                       
                                                                                                        
  **位置 1：在 ChatCompletions() 方法开始处**（约第 84 行）                                             
  ```go                                                                                                 
  // 创建请求日志上下文                                                                                 
  logCtx := &RequestLogContext{                                                                         
  RequestID:   uuid.New().String(),                                                                     
  TokenID:     tokenIDStr,                                                                              
  UserName:    userName,                                                                                
  Model:       req.Model,                                                                               
  Mode:        mode,                                                                                    
  Stream:      req.Stream,                                                                              
  RequestAt:   time.Now(),                                                                              
  EnableConvLogging: h.shouldLogConversation(tokenIDStr),                                               
  SystemPrompt: extractSystemPrompt(req.Messages),                                                      
  Messages:     req.Messages,                                                                           
  }                                                                                                     
  c.Set("log_context", logCtx)                                                                          
  ```                                                                                                   
                                                                                                        
  **位置 2：在响应处理方法中提取 token usage**                                                          
  - `handleAPIResponseEnhanced()` - 第 547-562 行                                                       
  - `streamAPIResponseEnhanced()` - 第 600-691 行                                                       
  - `handleWebResponseEnhanced()` - 第 693-754 行                                                       
  - `streamWebResponseEnhanced()` - 第 756-842 行                                                       
                                                                                                        
  **修改示例**（handleAPIResponseEnhanced）：                                                           
  ```go                                                                                                 
  func (h *EnhancedProxyHandler) handleAPIResponseEnhanced(...) {                                       
  logCtx, _ := c.Get("log_context").(*RequestLogContext)                                                
                                                                                                        
  // ... 现有代码解析响应 ...                                                                           
                                                                                                        
  if resp.StatusCode == http.StatusOK {                                                                 
  // 提取 token usage                                                                                   
  logCtx.PromptTokens = anthropicResp.Usage.InputTokens                                                 
  logCtx.CompletionTokens = anthropicResp.Usage.OutputTokens                                            
  logCtx.Completion = extractCompletionText(anthropicResp)                                              
  logCtx.ResponseAt = time.Now()                                                                        
  }                                                                                                     
                                                                                                        
  // 异步记录日志                                                                                       
  go h.logRequest(logCtx, resp.StatusCode, nil)                                                         
                                                                                                        
  // ... 现有代码返回响应 ...                                                                           
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  **新增辅助方法**：                                                                                    
  ```go                                                                                                 
  func (h *EnhancedProxyHandler) shouldLogConversation(tokenID string) bool {                           
  token, err := h.store.GetToken(tokenID)                                                               
  if err != nil || token == nil {                                                                       
  return false                                                                                          
  }                                                                                                     
  return token.EnableConversationLogging                                                                
  }                                                                                                     
                                                                                                        
  func (h *EnhancedProxyHandler) logRequest(logCtx *RequestLogContext, statusCode int, err error) {     
  entry := buildLogEntry(logCtx, statusCode, err)                                                       
  if err := h.requestLogger.LogRequest(entry); err != nil {                                             
  log.Error().Err(err).Msg("failed to queue request log")                                               
  }                                                                                                     
  }                                                                                                     
                                                                                                        
  func buildLogEntry(logCtx *RequestLogContext, statusCode int, err error) *service.LogEntry {          
  // 构建 RequestLog                                                                                    
  // 如果 EnableConvLogging，构建 ConversationContent                                                   
  // 返回 LogEntry                                                                                      
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  ### 1.5 API 端点                                                                                      
                                                                                                        
  #### internal/handler/request_logs.go（新文件）                                                       
  ```go                                                                                                 
  type RequestLogsHandler struct {                                                                      
  store *store.Store                                                                                    
  }                                                                                                     
                                                                                                        
  // GET /api/logs/requests                                                                             
  func (h *RequestLogsHandler) ListRequestLogs(c *gin.Context)                                          
                                                                                                        
  // GET /api/logs/requests/:id                                                                         
  func (h *RequestLogsHandler) GetRequestLog(c *gin.Context)                                            
  ```                                                                                                   
                                                                                                        
  #### 路由注册（cmd/server/main.go）                                                                   
  ```go                                                                                                 
  requestLogsHandler := handler.NewRequestLogsHandler(store)                                            
  api.GET("/logs/requests", requestLogsHandler.ListRequestLogs)                                         
  api.GET("/logs/requests/:id", requestLogsHandler.GetRequestLog)                                       
  ```                                                                                                   
                                                                                                        
  ---                                                                                                   
                                                                                                        
  ## 第二阶段：统计聚合和可视化 (Priority 2)                                                            
                                                                                                        
  ### 2.1 聚合统计表                                                                                    
                                                                                                        
  #### usage_stats_daily 表                                                                             
  ```sql                                                                                                
  CREATE TABLE usage_stats_daily (                                                                      
  id INTEGER PRIMARY KEY AUTOINCREMENT,                                                                 
  stat_date DATE NOT NULL,                                                                              
  token_id TEXT,                                                                                        
  account_id TEXT,                                                                                      
  mode TEXT,                                                                                            
  model TEXT,                                                                                           
  request_count INTEGER DEFAULT 0,                                                                      
  success_count INTEGER DEFAULT 0,                                                                      
  error_count INTEGER DEFAULT 0,                                                                        
  total_prompt_tokens INTEGER DEFAULT 0,                                                                
  total_completion_tokens INTEGER DEFAULT 0,                                                            
  total_tokens INTEGER DEFAULT 0,                                                                       
  avg_duration_ms INTEGER DEFAULT 0,                                                                    
  avg_ttft_ms INTEGER DEFAULT 0,                                                                        
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,                                                        
  UNIQUE(stat_date, token_id, account_id, mode, model)                                                  
  );                                                                                                    
                                                                                                        
  CREATE INDEX idx_usage_stats_date ON usage_stats_daily(stat_date DESC);                               
  CREATE INDEX idx_usage_stats_token ON usage_stats_daily(token_id, stat_date DESC);                    
  CREATE INDEX idx_usage_stats_account ON usage_stats_daily(account_id, stat_date DESC);                
  ```                                                                                                   
                                                                                                        
  ### 2.2 后台聚合任务                                                                                  
                                                                                                        
  #### internal/service/stats_aggregator.go（新文件）                                                   
  ```go                                                                                                 
  type StatsAggregator struct {                                                                         
  store    *store.Store                                                                                 
  interval time.Duration                                                                                
  ticker   *time.Ticker                                                                                 
  }                                                                                                     
                                                                                                        
  func (sa *StatsAggregator) Start(ctx context.Context) error {                                         
  ticker := time.NewTicker(sa.interval)                                                                 
  go func() {                                                                                           
  for {                                                                                                 
  select {                                                                                              
  case <-ticker.C:                                                                                      
  sa.runAggregation()                                                                                   
  case <-ctx.Done():                                                                                    
  return                                                                                                
  }                                                                                                     
  }                                                                                                     
  }()                                                                                                   
  }                                                                                                     
                                                                                                        
  func (sa *StatsAggregator) runAggregation() error {                                                   
  // 聚合昨天的数据                                                                                     
  yesterday := time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)                                    
                                                                                                        
  query := `                                                                                            
  INSERT OR REPLACE INTO usage_stats_daily                                                              
  SELECT                                                                                                
  NULL,                                                                                                 
  DATE(request_at) as stat_date,                                                                        
  token_id,                                                                                             
  account_id,                                                                                           
  mode,                                                                                                 
  model,                                                                                                
  COUNT(*) as request_count,                                                                            
  SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as success_count,                                        
  SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) as error_count,                                          
  SUM(prompt_tokens) as total_prompt_tokens,                                                            
  SUM(completion_tokens) as total_completion_tokens,                                                    
  SUM(total_tokens) as total_tokens,                                                                    
  AVG(duration_ms) as avg_duration_ms,                                                                  
  AVG(ttft_ms) as avg_ttft_ms,                                                                          
  datetime('now')                                                                                       
  FROM request_logs                                                                                     
  WHERE DATE(request_at) = ?                                                                            
  GROUP BY DATE(request_at), token_id, account_id, mode, model                                          
  `                                                                                                     
                                                                                                        
  _, err := sa.store.db.Exec(query, yesterday.Format("2006-01-02"))                                     
  return err                                                                                            
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  **启动位置**：cmd/server/main.go                                                                      
  ```go                                                                                                 
  // 启动统计聚合器（每天凌晨1点运行）                                                                  
  statsAgg := service.NewStatsAggregator(store, 24*time.Hour)                                           
  go statsAgg.Start(ctx)                                                                                
  ```                                                                                                   
                                                                                                        
  ### 2.3 统计 API                                                                                      
                                                                                                        
  #### internal/handler/stats.go（新文件）                                                              
  ```go                                                                                                 
  type StatsHandler struct {                                                                            
  store *store.Store                                                                                    
  }                                                                                                     
                                                                                                        
  // GET /api/stats/tokens/:id - Token 用量统计                                                         
  func (h *StatsHandler) GetTokenStats(c *gin.Context)                                                  
                                                                                                        
  // GET /api/stats/tokens/:id/trend - Token 趋势（日统计）                                             
  func (h *StatsHandler) GetTokenTrend(c *gin.Context)                                                  
                                                                                                        
  // GET /api/stats/accounts/:id - Account 用量统计                                                     
  func (h *StatsHandler) GetAccountStats(c *gin.Context)                                                
                                                                                                        
  // GET /api/stats/accounts/:id/trend - Account 趋势                                                   
  func (h *StatsHandler) GetAccountTrend(c *gin.Context)                                                
                                                                                                        
  // GET /api/stats/overview - 全局统计概览                                                             
  func (h *StatsHandler) GetOverview(c *gin.Context)                                                    
  ```                                                                                                   
                                                                                                        
  #### internal/store/usage_stats.go（新文件）                                                          
  ```go                                                                                                 
  type UsageStats struct {                                                                              
  StatDate              time.Time                                                                       
  TokenID               sql.NullString                                                                  
  AccountID             sql.NullString                                                                  
  Mode                  string                                                                          
  Model                 string                                                                          
  RequestCount          int                                                                             
  SuccessCount          int                                                                             
  ErrorCount            int                                                                             
  TotalPromptTokens     int                                                                             
  TotalCompletionTokens int                                                                             
  TotalTokens           int                                                                             
  AvgDurationMs         int                                                                             
  AvgTTFTMs             int                                                                             
  }                                                                                                     
                                                                                                        
  func (s *Store) GetTokenStats(tokenID string, from, to time.Time) (*AggregatedStats, error)           
  func (s *Store) GetTokenTrend(tokenID string, days int) ([]*DailyStats, error)                        
  func (s *Store) GetAccountStats(accountID string, from, to time.Time) (*AggregatedStats, error)       
  func (s *Store) GetAccountTrend(accountID string, days int) ([]*DailyStats, error)                    
  func (s *Store) GetGlobalOverview(from, to time.Time) (*GlobalStats, error)                           
  ```                                                                                                   
                                                                                                        
  ### 2.4 前端实现                                                                                      
                                                                                                        
  #### web/src/api/types.ts（新增类型）                                                                 
  ```typescript                                                                                         
  export interface RequestLog {                                                                         
  id: string;                                                                                           
  token_id: string;                                                                                     
  account_id?: string;                                                                                  
  user_name: string;                                                                                    
  mode: 'web' | 'api';                                                                                  
  model: string;                                                                                        
  stream: boolean;                                                                                      
  request_at: string;                                                                                   
  response_at?: string;                                                                                 
  duration_ms?: number;                                                                                 
  ttft_ms?: number;                                                                                     
  prompt_tokens: number;                                                                                
  completion_tokens: number;                                                                            
  total_tokens: number;                                                                                 
  status_code: number;                                                                                  
  success: boolean;                                                                                     
  error_message?: string;                                                                               
  }                                                                                                     
                                                                                                        
  export interface UsageStats {                                                                         
  request_count: number;                                                                                
  success_count: number;                                                                                
  error_count: number;                                                                                  
  total_prompt_tokens: number;                                                                          
  total_completion_tokens: number;                                                                      
  total_tokens: number;                                                                                 
  avg_duration_ms: number;                                                                              
  avg_ttft_ms: number;                                                                                  
  success_rate: number;                                                                                 
  }                                                                                                     
                                                                                                        
  export interface DailyTrend {                                                                         
  date: string;                                                                                         
  request_count: number;                                                                                
  success_count: number;                                                                                
  total_tokens: number;                                                                                 
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  #### web/src/hooks/useRequestLogs.ts（新文件）                                                        
  ```typescript                                                                                         
  export function useRequestLogs(filter: RequestLogFilter) {                                            
  return useQuery({                                                                                     
  queryKey: ['requestLogs', filter],                                                                    
  queryFn: () => apiClient.listRequestLogs(filter),                                                     
  keepPreviousData: true,                                                                               
  });                                                                                                   
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  #### web/src/hooks/useUsageStats.ts（新文件）                                                         
  ```typescript                                                                                         
  export function useTokenStats(tokenId: string, from?: Date, to?: Date) {                              
  return useQuery({                                                                                     
  queryKey: ['tokenStats', tokenId, from, to],                                                          
  queryFn: () => apiClient.getTokenStats(tokenId, from, to),                                            
  staleTime: 60000,                                                                                     
  });                                                                                                   
  }                                                                                                     
                                                                                                        
  export function useTokenTrend(tokenId: string, days: number = 30) {                                   
  return useQuery({                                                                                     
  queryKey: ['tokenTrend', tokenId, days],                                                              
  queryFn: () => apiClient.getTokenTrend(tokenId, days),                                                
  });                                                                                                   
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  #### web/src/pages/UsageStats.tsx（新文件）                                                           
  主要功能：                                                                                            
  - 概览卡片：总请求数、成功率、总 Token 使用量                                                         
  - 日期范围选择器                                                                                      
  - 趋势图表（使用 Recharts）                                                                           
  - 按模型分组的统计表格                                                                                
  - 筛选器：Token、Account、Mode、Model                                                                 
                                                                                                        
  #### web/src/pages/RequestLogs.tsx（新文件）                                                          
  主要功能：                                                                                            
  - 分页表格展示所有请求日志                                                                            
  - 筛选器：Token ID、Account ID、日期范围、状态、Mode、Model                                           
  - 排序：按时间、状态、Token 使用量                                                                    
  - 点击行查看详情                                                                                      
  - 导出功能（CSV）                                                                                     
                                                                                                        
  #### 修改现有页面                                                                                     
                                                                                                        
  **web/src/pages/Tokens.tsx**                                                                          
  - 在 Token 详情对话框中添加"用量统计"标签页                                                           
  - 显示该 Token 的请求趋势图表                                                                         
  - 显示该 Token 的总计数据                                                                             
  - 添加"启用对话记录"开关设置                                                                          
                                                                                                        
  **web/src/pages/Dashboard.tsx**                                                                       
  - 添加"请求活动"卡片（今日请求数、成功率）                                                            
  - 添加"Token 使用量"卡片（今日总 Token）                                                              
  - 添加"最近请求"列表（最新 10 条）                                                                    
  - 添加趋势图表（最近 7 天请求量）                                                                     
                                                                                                        
  ---                                                                                                   
                                                                                                        
  ## 第三阶段：对话内容查看器 (Priority 3)                                                              
                                                                                                        
  ### 3.1 对话 API                                                                                      
                                                                                                        
  #### internal/handler/conversations.go（新文件）                                                      
  ```go                                                                                                 
  type ConversationsHandler struct {                                                                    
  store *store.Store                                                                                    
  }                                                                                                     
                                                                                                        
  // GET /api/conversations - 列出对话（分页）                                                          
  func (h *ConversationsHandler) ListConversations(c *gin.Context)                                      
                                                                                                        
  // GET /api/conversations/:id - 获取单个对话                                                          
  func (h *ConversationsHandler) GetConversation(c *gin.Context)                                        
                                                                                                        
  // GET /api/conversations/search - 全文搜索                                                           
  func (h *ConversationsHandler) SearchConversations(c *gin.Context)                                    
                                                                                                        
  // DELETE /api/conversations/:id - 删除对话                                                           
  func (h *ConversationsHandler) DeleteConversation(c *gin.Context)                                     
  ```                                                                                                   
                                                                                                        
  ### 3.2 Token 设置 API                                                                                
                                                                                                        
  #### internal/handler/token.go（修改）                                                                
  ```go                                                                                                 
  // PUT /api/token/:id/settings - 更新 Token 设置                                                      
  func (h *TokenHandler) UpdateTokenSettings(c *gin.Context) {                                          
  // 允许更新 enable_conversation_logging 字段                                                          
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  ### 3.3 前端对话查看器                                                                                
                                                                                                        
  #### web/src/hooks/useConversations.ts（新文件）                                                      
  ```typescript                                                                                         
  export function useConversations(filter: ConversationFilter) {                                        
  return useQuery({                                                                                     
  queryKey: ['conversations', filter],                                                                  
  queryFn: () => apiClient.listConversations(filter),                                                   
  keepPreviousData: true,                                                                               
  });                                                                                                   
  }                                                                                                     
                                                                                                        
  export function useConversationSearch(query: string) {                                                
  return useQuery({                                                                                     
  queryKey: ['conversationSearch', query],                                                              
  queryFn: () => apiClient.searchConversations(query),                                                  
  enabled: query.length > 2,                                                                            
  });                                                                                                   
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  #### web/src/pages/Conversations.tsx（新文件）                                                        
  主要功能：                                                                                            
  - 全文搜索输入框（带防抖）                                                                            
  - 分页列表展示对话                                                                                    
  - 每条对话显示：时间、模型、Prompt 预览（前 100 字符）                                                
  - 点击查看完整对话详情                                                                                
  - 删除按钮（带确认对话框）                                                                            
  - 导出功能                                                                                            
                                                                                                        
  #### web/src/pages/ConversationDetail.tsx（新文件）                                                   
  主要功能：                                                                                            
  - 显示完整对话内容                                                                                    
  - 系统提示词（如果有）                                                                                
  - 对话历史（多轮对话）                                                                                
  - 用户当前输入                                                                                        
  - 模型输出                                                                                            
  - 代码语法高亮（使用 Prism.js）                                                                       
  - 复制按钮（Prompt 和 Completion）                                                                    
  - 元数据显示：时间戳、模型、Token 使用量、响应时间                                                    
  - 链接到相关的请求日志                                                                                
                                                                                                        
  ---                                                                                                   
                                                                                                        
  ## 第四阶段：优化和高级功能 (Priority 4)                                                              
                                                                                                        
  ### 4.1 压缩服务                                                                                      
                                                                                                        
  #### internal/service/conversation_compressor.go（新文件）                                            
  ```go                                                                                                 
  type ConversationCompressor struct {                                                                  
  store       *store.Store                                                                              
  interval    time.Duration                                                                             
  compressAge time.Duration  // 压缩 N 天前的对话                                                       
  }                                                                                                     
                                                                                                        
  func (cc *ConversationCompressor) Start(ctx context.Context) error {                                  
  ticker := time.NewTicker(cc.interval)                                                                 
  go func() {                                                                                           
  for {                                                                                                 
  select {                                                                                              
  case <-ticker.C:                                                                                      
  cc.compressOldConversations()                                                                         
  case <-ctx.Done():                                                                                    
  return                                                                                                
  }                                                                                                     
  }                                                                                                     
  }()                                                                                                   
  }                                                                                                     
                                                                                                        
  func (cc *ConversationCompressor) compressOldConversations() error {                                  
  cutoffDate := time.Now().AddDate(0, 0, -int(cc.compressAge.Hours()/24))                               
                                                                                                        
  // 查询未压缩的旧对话                                                                                 
  rows, _ := cc.store.db.Query(`                                                                        
  SELECT id, prompt, completion, messages_json, system_prompt                                           
  FROM conversation_contents                                                                            
  WHERE is_compressed = 0 AND created_at < ?                                                            
  LIMIT 1000                                                                                            
  `, cutoffDate)                                                                                        
                                                                                                        
  for rows.Next() {                                                                                     
  // 使用 gzip 压缩文本字段                                                                             
  // 更新数据库，设置 is_compressed = 1                                                                 
  }                                                                                                     
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  **压缩策略**：                                                                                        
  - 压缩 7 天前的对话                                                                                   
  - 使用 gzip 压缩（预期 60-80% 存储节省）                                                              
  - 读取时透明解压缩                                                                                    
                                                                                                        
  ### 4.2 导出功能                                                                                      
                                                                                                        
  #### internal/handler/request_logs.go（添加方法）                                                     
  ```go                                                                                                 
  // GET /api/logs/requests/export                                                                      
  func (h *RequestLogsHandler) ExportRequestLogs(c *gin.Context) {                                      
  // 支持 CSV 和 JSON 格式                                                                              
  // 流式导出，避免内存溢出                                                                             
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  #### internal/handler/conversations.go（添加方法）                                                    
  ```go                                                                                                 
  // POST /api/conversations/export                                                                     
  func (h *ConversationsHandler) ExportConversations(c *gin.Context) {                                  
  // 导出选定的对话为 JSON 或 JSONL                                                                     
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  ### 4.3 性能优化                                                                                      
                                                                                                        
  **数据库优化**：                                                                                      
  ```go                                                                                                 
  // 在 store 初始化时启用 WAL 模式和优化设置                                                           
  func New(dbPath string) (*Store, error) {                                                             
  db, err := sql.Open("sqlite3",                                                                        
  dbPath+"?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-64000")                                   
  // ...                                                                                                
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  **批量写入优化**：                                                                                    
  ```go                                                                                                 
  func (rl *RequestLogger) processQueue(ctx context.Context) {                                          
  batch := make([]*LogEntry, 0, rl.batchSize)                                                           
                                                                                                        
  for {                                                                                                 
  select {                                                                                              
  case entry := <-rl.queue:                                                                             
  batch = append(batch, entry)                                                                          
                                                                                                        
  if len(batch) >= rl.batchSize {                                                                       
  rl.writeBatch(batch)                                                                                  
  batch = batch[:0]                                                                                     
  }                                                                                                     
  case <-time.After(5 * time.Second):                                                                   
  if len(batch) > 0 {                                                                                   
  rl.writeBatch(batch)                                                                                  
  batch = batch[:0]                                                                                     
  }                                                                                                     
  }                                                                                                     
  }                                                                                                     
  }                                                                                                     
                                                                                                        
  func (rl *RequestLogger) writeBatch(entries []*LogEntry) error {                                      
  tx, _ := rl.store.db.Begin()                                                                          
                                                                                                        
  stmt, _ := tx.Prepare(`INSERT INTO request_logs (...) VALUES (?, ?, ...)`)                            
  for _, entry := range entries {                                                                       
  stmt.Exec(entry.Log.ID, entry.Log.TokenID, ...)                                                       
  }                                                                                                     
  stmt.Close()                                                                                          
                                                                                                        
  // 如果有对话记录                                                                                     
  if hasConversations {                                                                                 
  stmt2, _ := tx.Prepare(`INSERT INTO conversation_contents (...) VALUES (?, ?, ...)`)                  
  // ...                                                                                                
  stmt2.Close()                                                                                         
  }                                                                                                     
                                                                                                        
  return tx.Commit()                                                                                    
  }                                                                                                     
  ```                                                                                                   
                                                                                                        
  ---                                                                                                   
                                                                                                        
  ## 验证和测试计划                                                                                     
                                                                                                        
  ### 端到端测试流程                                                                                    
                                                                                                        
  1. **日志记录测试**                                                                                   
  - 发送测试请求到 `/v1/chat/completions`                                                               
  - 验证 `request_logs` 表中有新记录                                                                    
  - 验证字段值正确（token_id, duration, tokens, status）                                                
  - 验证异步日志不阻塞请求响应                                                                          
                                                                                                        
  2. **对话记录测试**                                                                                   
  - 创建一个启用对话记录的 Token                                                                        
  - 发送带系统提示词和多轮对话的请求                                                                    
  - 验证 `conversation_contents` 表中有记录                                                             
  - 验证内容完整性（system prompt, messages, prompt, completion）                                       
  - 验证未启用对话记录的 Token 不会创建对话记录                                                         
                                                                                                        
  3. **统计聚合测试**                                                                                   
  - 手动运行 StatsAggregator                                                                            
  - 验证 `usage_stats_daily` 表中有聚合数据                                                             
  - 验证计算正确（count, sum, avg）                                                                     
  - 验证 API 返回的统计数据与数据库一致                                                                 
                                                                                                        
  4. **前端集成测试**                                                                                   
  - 访问 Usage Stats 页面，验证图表正确显示                                                             
  - 访问 Request Logs 页面，验证日志列表和筛选功能                                                      
  - 访问 Conversations 页面，验证搜索和查看功能                                                         
  - 测试 Token 设置中的对话记录开关                                                                     
                                                                                                        
  5. **性能测试**                                                                                       
  - 压力测试：发送 1000 个并发请求                                                                      
  - 验证日志 queue 不会溢出                                                                             
  - 验证请求延迟无明显增加（<10ms）                                                                     
  - 监控数据库写入性能                                                                                  
                                                                                                        
  ### 关键指标                                                                                          
                                                                                                        
  - **请求延迟影响**：< 10ms（日志记录异步，不应影响）                                                  
  - **日志写入延迟**：< 1s（从请求完成到数据库写入）                                                    
  - **队列容量**：10000 条目，正常情况下应保持在 < 50%                                                  
  - **数据库大小**：启用对话记录后，预期每天增长 100-500MB（取决于请求量）                              
  - **查询性能**：统计查询 < 500ms，日志列表查询 < 200ms                                                
                                                                                                        
  ---                                                                                                   
                                                                                                        
  ## 关键文件清单                                                                                       
                                                                                                        
  ### 后端新增文件                                                                                      
  1. `internal/store/request_log.go` - 请求日志数据模型和操作                                           
  2. `internal/store/conversation.go` - 对话内容数据模型和操作                                          
  3. `internal/store/usage_stats.go` - 统计聚合数据模型和操作                                           
  4. `internal/service/request_logger.go` - 异步日志服务                                                
  5. `internal/service/stats_aggregator.go` - 统计聚合后台任务                                          
  6. `internal/service/conversation_compressor.go` - 对话压缩服务                                       
  7. `internal/handler/request_logs.go` - 请求日志 API                                                  
  8. `internal/handler/stats.go` - 统计 API                                                             
  9. `internal/handler/conversations.go` - 对话内容 API                                                 
                                                                                                        
  ### 后端修改文件                                                                                      
  1. `internal/store/sqlite.go` - 添加数据库迁移                                                        
  2. `internal/handler/enhanced_proxy.go` - 集成日志记录                                                
  3. `internal/handler/token.go` - 添加 Token 设置 API                                                  
  4. `cmd/server/main.go` - 注册路由和启动后台服务                                                      
                                                                                                        
  ### 前端新增文件                                                                                      
  1. `web/src/hooks/useRequestLogs.ts` - 请求日志 hooks                                                 
  2. `web/src/hooks/useUsageStats.ts` - 统计 hooks                                                      
  3. `web/src/hooks/useConversations.ts` - 对话内容 hooks                                               
  4. `web/src/pages/UsageStats.tsx` - 用量统计页面                                                      
  5. `web/src/pages/RequestLogs.tsx` - 请求日志页面                                                     
  6. `web/src/pages/Conversations.tsx` - 对话列表页面                                                   
  7. `web/src/pages/ConversationDetail.tsx` - 对话详情页面                                              
                                                                                                        
  ### 前端修改文件                                                                                      
  1. `web/src/api/types.ts` - 添加新类型定义                                                            
  2. `web/src/api/client.ts` - 添加 API 方法                                                            
  3. `web/src/pages/Tokens.tsx` - 添加用量统计标签                                                      
  4. `web/src/pages/Dashboard.tsx` - 添加请求活动统计                                                   
  5. `web/src/App.tsx` - 添加新路由                                                                     
  6. `web/src/components/Layout.tsx` - 添加导航链接                                                     
                                                                                                        
  ---                                                                                                   
                                                                                                        
  ## 配置需求                                                                                           
                                                                                                        
  ### config.yaml 新增配置                                                                              
  ```yaml                                                                                               
  logging:                                                                                              
  enabled: true                                                                                         
  buffer_size: 10000                                                                                    
  workers: 4                                                                                            
  batch_size: 100                                                                                       
                                                                                                        
  conversations:                                                                                        
  enabled: true                                                                                         
  compress_after_days: 7                                                                                
                                                                                                        
  stats:                                                                                                
  aggregation_enabled: true                                                                             
  aggregation_interval: "24h"  # 每24小时运行一次                                                       
  ```                                                                                                   
                                                                                                        
  ---                                                                                                   
                                                                                                        
  ## 实施顺序                                                                                           
                                                                                                        
  1. **第一阶段（1-2周）**：数据库模式 + 核心日志记录                                                   
  - 完成所有数据库表和索引                                                                              
  - 实现 Store 层（request_log.go, conversation.go）                                                    
  - 实现异步日志服务（request_logger.go）                                                               
  - 集成到 enhanced_proxy.go                                                                            
  - 实现基础 API（listRequestLogs, getRequestLog）                                                      
  - 验证日志记录正常工作                                                                                
                                                                                                        
  2. **第二阶段（1周）**：统计聚合和可视化                                                              
  - 实现聚合表和后台任务                                                                                
  - 实现统计 API                                                                                        
  - 前端：UsageStats 页面和 RequestLogs 页面                                                            
  - 增强 Tokens 和 Dashboard 页面                                                                       
                                                                                                        
  3. **第三阶段（1周）**：对话内容查看器                                                                
  - 实现全文搜索                                                                                        
  - 实现对话 API                                                                                        
  - 前端：Conversations 和 ConversationDetail 页面                                                      
  - 实现 Token 设置 API                                                                                 
                                                                                                        
  4. **第四阶段（1周）**：优化和高级功能                                                                
  - 压缩服务                                                                                            
  - 导出功能                                                                                            
  - 性能优化和测试                                                                                      
  - 文档和用户指南       
