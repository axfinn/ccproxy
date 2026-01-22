import { useState, useCallback, useEffect } from 'react';
import { apiClient } from '@/api/client';

export function useAuth() {
  const [isAuthenticated, setIsAuthenticated] = useState(apiClient.isAuthenticated());
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setIsAuthenticated(apiClient.isAuthenticated());
  }, []);

  const login = useCallback(async (adminKey: string) => {
    setIsLoading(true);
    setError(null);
    try {
      const success = await apiClient.verifyAdminKey(adminKey);
      if (success) {
        setIsAuthenticated(true);
        return true;
      } else {
        setError('Invalid admin key');
        return false;
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
      return false;
    } finally {
      setIsLoading(false);
    }
  }, []);

  const logout = useCallback(() => {
    apiClient.clearAdminKey();
    setIsAuthenticated(false);
  }, []);

  return {
    isAuthenticated,
    isLoading,
    error,
    login,
    logout,
  };
}
