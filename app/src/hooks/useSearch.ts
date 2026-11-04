import { useCallback, useEffect, useRef, useState } from 'react';

import { createApiClient, type ApiClient } from '../api/client';
import type { Mode, Place } from '../api/types';
import { API_BASE_URL } from '../config';
import { addPoint, nextPointId, removePoint, movePoint, type RnPoint } from './pointsLogic';

const DEBOUNCE_MS = 300;

/**
 * 搜索状态：防抖 300ms、过期请求作废、错误吞进 error 状态。
 * useSearch 是 UI 无关的（api client 注入便于测试）。
 */
export function useSearch(apiInput?: ApiClient) {
  // api 必须跨渲染稳定：默认参数每次渲染都会新建客户端，effect 会无限重跑
  const apiRef = useRef<ApiClient | null>(null);
  if (apiInput) apiRef.current = apiInput;
  else if (!apiRef.current) apiRef.current = createApiClient(API_BASE_URL);
  const api = apiRef.current;

  const [query, setQuery] = useState('');
  const [city, setCity] = useState('');
  const [results, setResults] = useState<Place[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const seqRef = useRef(0);

  useEffect(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    const q = query.trim();
    if (!q) {
      setResults([]);
      setLoading(false);
      setError(null);
      return;
    }
    setLoading(true);
    timerRef.current = setTimeout(async () => {
      const seq = ++seqRef.current;
      try {
        const res = await api.search(q, city || undefined);
        if (seq !== seqRef.current) return; // 过期请求作废
        setResults(res.places);
        setError(null);
      } catch (e) {
        if (seq !== seqRef.current) return;
        setResults([]);
        setError(e instanceof Error ? e.message : String(e));
      } finally {
        if (seq === seqRef.current) setLoading(false);
      }
    }, DEBOUNCE_MS);
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [query, city, api]);

  return { query, setQuery, city, setCity, results, loading, error };
}

/** 点列表状态：增删/上下移/段方式 + 地图 marker id 管理 */
export function usePoints() {
  const [points, setPoints] = useState<RnPoint[]>([]);

  const add = useCallback((p: Omit<RnPoint, 'id'>) => {
    setPoints((prev) => addPoint(prev, { ...p, id: nextPointId(prev) }));
  }, []);
  const remove = useCallback((id: string) => {
    setPoints((prev) => removePoint(prev, id));
  }, []);
  const move = useCallback((id: string, dir: -1 | 1) => {
    setPoints((prev) => movePoint(prev, id, dir));
  }, []);
  const setLegMode = useCallback((id: string, legMode: Mode) => {
    setPoints((prev) => prev.map((p) => (p.id === id ? { ...p, legMode } : p)));
  }, []);

  return { points, add, remove, move, setLegMode };
}
