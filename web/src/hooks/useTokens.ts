import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/api/client';
import type { GenerateTokenRequest } from '@/api/types';

export function useTokens() {
  return useQuery({
    queryKey: ['tokens'],
    queryFn: () => apiClient.listTokens(),
    staleTime: 30000,
  });
}

export function useGenerateToken() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (req: GenerateTokenRequest) => apiClient.generateToken(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tokens'] });
    },
  });
}

export function useRevokeToken() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => apiClient.revokeToken({ id }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tokens'] });
    },
  });
}
