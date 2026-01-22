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

// Generic API response
export interface ApiError {
  error: string;
}

export interface ApiMessage {
  message: string;
}
