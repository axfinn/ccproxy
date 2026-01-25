import { useState, useEffect } from 'react';
import { apiClient } from '../api/client';
import type { ConversationListResponse, ConversationFilter, ConversationContent, ConversationSearchResponse } from '../api/types';

export function useConversations(filter: ConversationFilter = {}) {
  const [data, setData] = useState<ConversationListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = async () => {
    try {
      setLoading(true);
      setError(null);
      const result = await apiClient.listConversations(filter);
      setData(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load conversations');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, [JSON.stringify(filter)]);

  return { data, loading, error, refetch: fetchData };
}

export function useConversation(id: string | null) {
  const [data, setData] = useState<ConversationContent | null>(null);
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
        const result = await apiClient.getConversation(id);
        setData(result);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load conversation');
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [id]);

  return { data, loading, error };
}

export function useConversationSearch(tokenId: string | null, query: string, enabled = true) {
  const [data, setData] = useState<ConversationSearchResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const search = async () => {
    if (!tokenId || !query || query.length < 2) {
      setData(null);
      return;
    }

    try {
      setLoading(true);
      setError(null);
      const result = await apiClient.searchConversations(tokenId, query);
      setData(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Search failed');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (enabled) {
      const timeoutId = setTimeout(search, 300); // Debounce
      return () => clearTimeout(timeoutId);
    }
  }, [tokenId, query, enabled]);

  return { data, loading, error, search };
}
