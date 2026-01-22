import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';

export interface Account {
  id: string;
  name: string;
  type: 'oauth' | 'session_key' | 'api_key';
  organization_id: string;
  expires_at: string | null;
  created_at: string;
  last_used_at: string | null;
  is_active: boolean;
  last_check_at: string | null;
  health_status: 'healthy' | 'unhealthy' | 'unknown';
  error_count: number;
  success_count: number;
}

export interface OAuthLoginRequest {
  session_key: string;
  name: string;
}

export interface SessionKeyAccountRequest {
  name: string;
  session_key: string;
  organization_id?: string;
}

export interface UpdateAccountRequest {
  name?: string;
  is_active?: boolean;
}

export function useAccounts() {
  return useQuery({
    queryKey: ['accounts'],
    queryFn: async () => {
      const adminKey = localStorage.getItem('adminKey');
      const response = await fetch('/api/account/list', {
        headers: {
          'X-Admin-Key': adminKey || '',
        },
      });
      if (!response.ok) throw new Error('Failed to fetch accounts');
      return response.json() as Promise<Account[]>;
    },
    staleTime: 30000,
  });
}

export function useOAuthLogin() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (req: OAuthLoginRequest) => {
      const adminKey = localStorage.getItem('adminKey');
      const response = await fetch('/api/account/oauth', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Admin-Key': adminKey || '',
        },
        body: JSON.stringify(req),
      });
      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'OAuth login failed');
      }
      return response.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
    },
  });
}

export function useCreateSessionKeyAccount() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (req: SessionKeyAccountRequest) => {
      const adminKey = localStorage.getItem('adminKey');
      const response = await fetch('/api/account/sessionkey', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Admin-Key': adminKey || '',
        },
        body: JSON.stringify(req),
      });
      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Failed to create account');
      }
      return response.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
    },
  });
}

export function useUpdateAccount() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ id, data }: { id: string; data: UpdateAccountRequest }) => {
      const adminKey = localStorage.getItem('adminKey');
      const response = await fetch(`/api/account/${id}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          'X-Admin-Key': adminKey || '',
        },
        body: JSON.stringify(data),
      });
      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Failed to update account');
      }
      return response.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
    },
  });
}

export function useDeleteAccount() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (id: string) => {
      const adminKey = localStorage.getItem('adminKey');
      const response = await fetch(`/api/account/${id}`, {
        method: 'DELETE',
        headers: {
          'X-Admin-Key': adminKey || '',
        },
      });
      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Failed to delete account');
      }
      return response.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
    },
  });
}

export function useDeactivateAccount() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (id: string) => {
      const adminKey = localStorage.getItem('adminKey');
      const response = await fetch(`/api/account/${id}/deactivate`, {
        method: 'POST',
        headers: {
          'X-Admin-Key': adminKey || '',
        },
      });
      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Failed to deactivate account');
      }
      return response.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
    },
  });
}

export function useRefreshToken() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (id: string) => {
      const adminKey = localStorage.getItem('adminKey');
      const response = await fetch(`/api/account/${id}/refresh`, {
        method: 'POST',
        headers: {
          'X-Admin-Key': adminKey || '',
        },
      });
      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Failed to refresh token');
      }
      return response.json();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
    },
  });
}

export function useCheckHealth() {
  return useMutation({
    mutationFn: async (id: string) => {
      const adminKey = localStorage.getItem('adminKey');
      const response = await fetch(`/api/account/${id}/check`, {
        method: 'POST',
        headers: {
          'X-Admin-Key': adminKey || '',
        },
      });
      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Health check failed');
      }
      return response.json();
    },
  });
}
