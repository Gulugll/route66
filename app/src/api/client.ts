import type { Mode, PlanRequest, PlanResult, RouteResponse, SearchResponse } from './types';

/** 网络层错误统一归一为 ApiError：status=0 表示请求根本没到达对端（超时/断网/DNS）。 */
export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

interface FetchLike {
  (url: string, init?: RequestInit): Promise<Response>;
}

/**
 * fetch 封装：baseURL 注入、超时、错误归一。
 * RN 的 fetch 没有 XMLHttpRequest.timeout，超时统一走 AbortController。
 */
export function createApiClient(baseURL: string, timeoutMs = 5000) {
  const doFetch: FetchLike = (...args) => fetch(...args);

  async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    let res: Response;
    try {
      res = await doFetch(baseURL + path, { ...init, signal: controller.signal });
    } catch (e) {
      const aborted = e instanceof Error && e.name === 'AbortError';
      throw new ApiError(
        0,
        aborted
          ? `请求超时（${timeoutMs}ms）：检查后端 ${baseURL} 是否可达`
          : `网络错误：${e instanceof Error ? e.message : String(e)}`,
      );
    } finally {
      clearTimeout(timer);
    }

    if (!res.ok) {
      let message = `HTTP ${res.status}`;
      try {
        const body = (await res.json()) as { error?: string };
        if (body?.error) message = body.error;
      } catch {
        try {
          const text = await res.text();
          if (text) message = text.slice(0, 200);
        } catch {
          // 保留默认 message
        }
      }
      throw new ApiError(res.status, message);
    }
    return (await res.json()) as T;
  }

  return {
    /** GET /search?q=&city= —— city 空表示全国搜索 */
    search(q: string, city?: string): Promise<SearchResponse> {
      const params = new URLSearchParams({ q });
      if (city) params.set('city', city);
      return request<SearchResponse>(`/search?${params.toString()}`);
    },

    /** POST /plan */
    plan(req: PlanRequest): Promise<PlanResult> {
      return request<PlanResult>('/plan', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(req),
      });
    },

    /**
     * GET /route —— 两点间真实路网轨迹。
     * 坐标 "lng,lat"（经度在前，与高德/后端 parsePoint 一致）。
     * polyline 为空数组时由调用方降级画直线。
     */
    route(originLngLat: string, destLngLat: string, mode: Mode): Promise<RouteResponse> {
      const params = new URLSearchParams({ origin: originLngLat, dest: destLngLat, mode });
      return request<RouteResponse>(`/route?${params.toString()}`);
    },
  };
}

export type ApiClient = ReturnType<typeof createApiClient>;
