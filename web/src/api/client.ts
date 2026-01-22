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
