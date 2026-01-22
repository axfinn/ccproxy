import { useQuery } from '@tanstack/react-query';
import { apiClient } from '@/api/client';

export function useApiKeys() {
  return useQuery({
    queryKey: ['apiKeys'],
    queryFn: () => apiClient.getKeyStats(),
    staleTime: 30000,
    refetchInterval: 60000,
  });
}
