import type {
  TokenListResponse,
  GenerateTokenRequest,
  GenerateTokenResponse,
  RevokeTokenRequest,
  SessionListResponse,
  AddSessionRequest,
  SessionInfo,
  KeyStatsResponse,
  ApiMessage,
  RequestLogListResponse,
  RequestLogFilter,
  RequestLog,
  ConversationListResponse,
  ConversationFilter,
  ConversationContent,
  ConversationSearchResponse,
  AggregatedStats,
  TrendResponse,
  GlobalStats,
  RealtimeStats,
  TopTokensResponse,
  TopModelsResponse,
  UpdateTokenSettingsRequest,
} from './types';

const API_BASE = '/api';

class ApiClient {
  private adminKey: string | null = null;

  setAdminKey(key: string) {
    this.adminKey = key;
    localStorage.setItem('adminKey', key);
  }

  getAdminKey(): string | null {
    if (!this.adminKey) {
      this.adminKey = localStorage.getItem('adminKey');
    }
    return this.adminKey;
  }

  clearAdminKey() {
    this.adminKey = null;
    localStorage.removeItem('adminKey');
  }

  isAuthenticated(): boolean {
    return !!this.getAdminKey();
  }

  private async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const adminKey = this.getAdminKey();
    if (!adminKey) {
      throw new Error('Not authenticated');
    }

    const response = await fetch(`${API_BASE}${endpoint}`, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        'X-Admin-Key': adminKey,
        ...options.headers,
      },
    });

    if (!response.ok) {
      const error = await response.json().catch(() => ({ error: 'Unknown error' }));
      throw new Error(error.error || `HTTP ${response.status}`);
    }

    return response.json();
  }

  // Token endpoints
  async listTokens(): Promise<TokenListResponse> {
    return this.request<TokenListResponse>('/token/list');
  }

  async generateToken(req: GenerateTokenRequest): Promise<GenerateTokenResponse> {
    return this.request<GenerateTokenResponse>('/token/generate', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async revokeToken(req: RevokeTokenRequest): Promise<ApiMessage> {
    return this.request<ApiMessage>('/token/revoke', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async updateTokenSettings(id: string, settings: UpdateTokenSettingsRequest): Promise<ApiMessage> {
    return this.request<ApiMessage>(`/token/${id}/settings`, {
      method: 'PUT',
      body: JSON.stringify(settings),
    });
  }

  // Session endpoints
  async listSessions(): Promise<SessionListResponse> {
    return this.request<SessionListResponse>('/session/list');
  }

  async addSession(req: AddSessionRequest): Promise<SessionInfo> {
    return this.request<SessionInfo>('/session/add', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async deleteSession(id: string): Promise<ApiMessage> {
    return this.request<ApiMessage>(`/session/${id}`, {
      method: 'DELETE',
    });
  }

  async deactivateSession(id: string): Promise<ApiMessage> {
    return this.request<ApiMessage>(`/session/${id}/deactivate`, {
      method: 'POST',
    });
  }

  // API Key endpoints
  async getKeyStats(): Promise<KeyStatsResponse> {
    return this.request<KeyStatsResponse>('/keys/stats');
  }

  // Request Logs endpoints
  async listRequestLogs(filter: RequestLogFilter = {}): Promise<RequestLogListResponse> {
    const params = new URLSearchParams();
    Object.entries(filter).forEach(([key, value]) => {
      if (value !== undefined && value !== null) {
        params.append(key, String(value));
      }
    });
    const query = params.toString() ? `?${params.toString()}` : '';
    return this.request<RequestLogListResponse>(`/logs/requests${query}`);
  }

  async getRequestLog(id: string): Promise<RequestLog> {
    return this.request<RequestLog>(`/logs/requests/${id}`);
  }

  async exportRequestLogs(filter: RequestLogFilter = {}, format: 'csv' | 'json' = 'csv'): Promise<Blob> {
    const params = new URLSearchParams({ ...filter as any, format });
    const adminKey = this.getAdminKey();
    if (!adminKey) throw new Error('Not authenticated');

    const response = await fetch(`${API_BASE}/logs/requests/export?${params.toString()}`, {
      headers: { 'X-Admin-Key': adminKey },
    });

    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return response.blob();
  }

  async deleteOldRequestLogs(days: number): Promise<ApiMessage> {
    return this.request<ApiMessage>(`/logs/requests/old?days=${days}`, {
      method: 'DELETE',
    });
  }

  // Conversations endpoints
  async listConversations(filter: ConversationFilter = {}): Promise<ConversationListResponse> {
    const params = new URLSearchParams();
    Object.entries(filter).forEach(([key, value]) => {
      if (value !== undefined && value !== null) {
        params.append(key, String(value));
      }
    });
    const query = params.toString() ? `?${params.toString()}` : '';
    return this.request<ConversationListResponse>(`/conversations${query}`);
  }

  async getConversation(id: string): Promise<ConversationContent> {
    return this.request<ConversationContent>(`/conversations/${id}`);
  }

  async searchConversations(tokenId: string, query: string, limit = 20): Promise<ConversationSearchResponse> {
    const params = new URLSearchParams({ token_id: tokenId, q: query, limit: String(limit) });
    return this.request<ConversationSearchResponse>(`/conversations/search?${params.toString()}`);
  }

  async deleteConversation(id: string): Promise<ApiMessage> {
    return this.request<ApiMessage>(`/conversations/${id}`, {
      method: 'DELETE',
    });
  }

  async exportConversations(filter: ConversationFilter = {}, format: 'json' | 'jsonl' = 'json'): Promise<Blob> {
    const params = new URLSearchParams({ ...filter as any, format });
    const adminKey = this.getAdminKey();
    if (!adminKey) throw new Error('Not authenticated');

    const response = await fetch(`${API_BASE}/conversations/export?${params.toString()}`, {
      headers: { 'X-Admin-Key': adminKey },
    });

    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return response.blob();
  }

  // Usage Stats endpoints
  async getTokenStats(tokenId: string, days?: number, fromDate?: string, toDate?: string): Promise<AggregatedStats> {
    const params = new URLSearchParams();
    if (days) params.append('days', String(days));
    if (fromDate) params.append('from_date', fromDate);
    if (toDate) params.append('to_date', toDate);
    const query = params.toString() ? `?${params.toString()}` : '';
    return this.request<AggregatedStats>(`/stats/tokens/${tokenId}${query}`);
  }

  async getTokenTrend(tokenId: string, days = 30): Promise<TrendResponse> {
    return this.request<TrendResponse>(`/stats/tokens/${tokenId}/trend?days=${days}`);
  }

  async getAccountStats(accountId: string, days?: number, fromDate?: string, toDate?: string): Promise<AggregatedStats> {
    const params = new URLSearchParams();
    if (days) params.append('days', String(days));
    if (fromDate) params.append('from_date', fromDate);
    if (toDate) params.append('to_date', toDate);
    const query = params.toString() ? `?${params.toString()}` : '';
    return this.request<AggregatedStats>(`/stats/accounts/${accountId}${query}`);
  }

  async getAccountTrend(accountId: string, days = 30): Promise<TrendResponse> {
    return this.request<TrendResponse>(`/stats/accounts/${accountId}/trend?days=${days}`);
  }

  async getOverview(days?: number, fromDate?: string, toDate?: string): Promise<GlobalStats> {
    const params = new URLSearchParams();
    if (days) params.append('days', String(days));
    if (fromDate) params.append('from_date', fromDate);
    if (toDate) params.append('to_date', toDate);
    const query = params.toString() ? `?${params.toString()}` : '';
    return this.request<GlobalStats>(`/stats/overview${query}`);
  }

  async getRealtimeStats(): Promise<RealtimeStats> {
    return this.request<RealtimeStats>('/stats/realtime');
  }

  async getTopTokens(days = 7, limit = 10): Promise<TopTokensResponse> {
    return this.request<TopTokensResponse>(`/stats/top/tokens?days=${days}&limit=${limit}`);
  }

  async getTopModels(days = 7): Promise<TopModelsResponse> {
    return this.request<TopModelsResponse>(`/stats/top/models?days=${days}`);
  }

  // Verify admin key
  async verifyAdminKey(key: string): Promise<boolean> {
    try {
      this.setAdminKey(key);
      await this.listTokens();
      return true;
    } catch {
      this.clearAdminKey();
      return false;
    }
  }
}

export const apiClient = new ApiClient();
