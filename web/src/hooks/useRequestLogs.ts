import { useState, useEffect } from 'react';
import { apiClient } from '../api/client';
import type { RequestLogListResponse, RequestLogFilter, RequestLog } from '../api/types';

export function useRequestLogs(filter: RequestLogFilter = {}) {
  const [data, setData] = useState<RequestLogListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = async () => {
    try {
      setLoading(true);
      setError(null);
      const result = await apiClient.listRequestLogs(filter);
      setData(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load request logs');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, [JSON.stringify(filter)]);

  return { data, loading, error, refetch: fetchData };
}

export function useRequestLog(id: string | null) {
  const [data, setData] = useState<RequestLog | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!id) {
      setData(null);
      return;
    }

    const fetchData = async () => {
      try {
        setLoading(true);
        setError(null);
        const result = await apiClient.getRequestLog(id);
        setData(result);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load request log');
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [id]);

  return { data, loading, error };
}
