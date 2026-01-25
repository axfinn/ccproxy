// Token types
export interface TokenInfo {
  id: string;
  name: string;
  mode: 'web' | 'api' | 'both';
  created_at: string;
  expires_at: string;
  revoked_at?: string;
  last_used_at?: string;
  is_valid: boolean;
  enable_conversation_logging?: boolean;
  total_requests?: number;
  total_tokens_used?: number;
}

export interface TokenListResponse {
  tokens: TokenInfo[];
}

export interface GenerateTokenRequest {
  name: string;
  expires_in?: string;
  mode?: 'web' | 'api' | 'both';
}

export interface GenerateTokenResponse {
  token: string;
  id: string;
  name: string;
  mode: string;
  expires_at: string;
}

export interface RevokeTokenRequest {
  id: string;
}

// Session types
export interface SessionInfo {
  id: string;
  name: string;
  organization_id: string;
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
  is_active: boolean;
}

export interface SessionListResponse {
  sessions: SessionInfo[];
}

export interface AddSessionRequest {
  name: string;
  session_key: string;
  organization_id?: string;
  expires_in?: string;
}

// API Key types
export interface KeyStats {
  key: string;
  requests: number;
  errors: number;
  last_used?: string;
  is_healthy: boolean;
}

export interface KeyStatsResponse {
  total_keys: number;
  healthy_keys: number;
  keys: KeyStats[];
}

// Request Log types
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
  conversation_id?: string;
}

export interface RequestLogFilter {
  token_id?: string;
  account_id?: string;
  user_name?: string;
  mode?: string;
  model?: string;
  success?: boolean;
  from_date?: string;
  to_date?: string;
  page?: number;
  limit?: number;
}

export interface RequestLogListResponse {
  logs: RequestLog[];
  total: number;
  page: number;
  limit: number;
}

// Conversation types
export interface ConversationContent {
  id: string;
  request_log_id: string;
  token_id: string;
  system_prompt?: string;
  messages_json: string;
  prompt: string;
  completion: string;
  created_at: string;
  is_compressed: boolean;
}

export interface ConversationFilter {
  token_id?: string;
  from_date?: string;
  to_date?: string;
  page?: number;
  limit?: number;
}

export interface ConversationListResponse {
  conversations: ConversationContent[];
  total: number;
  page: number;
  limit: number;
}

export interface ConversationSearchResponse {
  query: string;
  token_id: string;
  conversations: ConversationContent[];
}

// Usage Stats types
export interface AggregatedStats {
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

export interface DailyStats {
  date: string;
  request_count: number;
  success_count: number;
  total_tokens: number;
}

export interface TrendResponse {
  token_id?: string;
  account_id?: string;
  days: number;
  trend: DailyStats[];
}

export interface GlobalStats {
  total_tokens: number;
  total_requests: number;
  total_users: number;
  active_tokens: number;
  by_mode: Record<string, AggregatedStats>;
  by_model: Record<string, AggregatedStats>;
}

export interface RealtimeStats {
  total_requests: number;
  success_count: number;
  error_count: number;
  total_tokens: number;
  avg_duration_ms: number;
  success_rate: number;
}

export interface TopToken {
  token_id: string;
  total_requests: number;
  total_tokens: number;
  success_count: number;
  error_count: number;
  success_rate: number;
}

export interface TopTokensResponse {
  days: number;
  limit: number;
  tokens: TopToken[];
}

export interface ModelStats {
  model: string;
  total_requests: number;
  total_tokens: number;
  success_count: number;
  success_rate: number;
}

export interface TopModelsResponse {
  days: number;
  models: ModelStats[];
}

// Token Settings
export interface UpdateTokenSettingsRequest {
  enable_conversation_logging?: boolean;
}

// Generic API response
export interface ApiError {
  error: string;
}

export interface ApiMessage {
  message: string;
}
