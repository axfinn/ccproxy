import { useState, useEffect } from 'react';
import { apiClient } from '../api/client';
import type { AggregatedStats, TrendResponse, GlobalStats, RealtimeStats, TopTokensResponse, TopModelsResponse } from '../api/types';

export function useTokenStats(tokenId: string | null, days?: number) {
  const [data, setData] = useState<AggregatedStats | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tokenId) {
      setData(null);
      return;
    }

    const fetchData = async () => {
      try {
        setLoading(true);
        setError(null);
        const result = await apiClient.getTokenStats(tokenId, days);
        setData(result);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load token stats');
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [tokenId, days]);

  return { data, loading, error };
}

export function useTokenTrend(tokenId: string | null, days = 30) {
  const [data, setData] = useState<TrendResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tokenId) {
      setData(null);
      return;
    }

    const fetchData = async () => {
      try {
        setLoading(true);
        setError(null);
        const result = await apiClient.getTokenTrend(tokenId, days);
        setData(result);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load token trend');
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [tokenId, days]);

  return { data, loading, error };
}

export function useAccountStats(accountId: string | null, days?: number) {
  const [data, setData] = useState<AggregatedStats | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!accountId) {
      setData(null);
      return;
    }

    const fetchData = async () => {
      try {
        setLoading(true);
        setError(null);
        const result = await apiClient.getAccountStats(accountId, days);
        setData(result);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load account stats');
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [accountId, days]);

  return { data, loading, error };
}

export function useGlobalStats(days?: number) {
  const [data, setData] = useState<GlobalStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = async () => {
    try {
      setLoading(true);
      setError(null);
      const result = await apiClient.getOverview(days);
      setData(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load global stats');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, [days]);

  return { data, loading, error, refetch: fetchData };
}

export function useRealtimeStats() {
  const [data, setData] = useState<RealtimeStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = async () => {
    try {
      setLoading(true);
      setError(null);
      const result = await apiClient.getRealtimeStats();
      setData(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load realtime stats');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
    const interval = setInterval(fetchData, 30000); // Refresh every 30s
    return () => clearInterval(interval);
  }, []);

  return { data, loading, error, refetch: fetchData };
}

export function useTopTokens(days = 7, limit = 10) {
  const [data, setData] = useState<TopTokensResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true);
        setError(null);
        const result = await apiClient.getTopTokens(days, limit);
        setData(result);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load top tokens');
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [days, limit]);

  return { data, loading, error };
}

export function useTopModels(days = 7) {
  const [data, setData] = useState<TopModelsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true);
        setError(null);
        const result = await apiClient.getTopModels(days);
        setData(result);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load top models');
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [days]);

  return { data, loading, error };
}
