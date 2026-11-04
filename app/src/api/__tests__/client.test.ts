import { ApiError, createApiClient } from '../client';

// 每个用例重写解码结构、路径写字面量：测试对齐的是"线上真实发出去的 JSON 字段名"，
// 不复用 src/api/types.ts 里的类型（那是实现，不是契约）。

describe('api client', () => {
  const origFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = origFetch;
  });

  function mockFetch(status: number, body: unknown) {
    globalThis.fetch = jest.fn().mockResolvedValue({
      ok: status >= 200 && status < 300,
      status,
      json: () => Promise.resolve(body),
      text: () => Promise.resolve(JSON.stringify(body)),
    }) as unknown as typeof fetch;
  }

  it('200 归一为解析后的数据', async () => {
    mockFetch(200, { places: [{ name: '天坛', address: '北京', lat: 39.8822, lng: 116.4066 }] });
    const api = createApiClient('http://localhost:7800');
    const res = await api.search('天坛');
    expect(res.places).toHaveLength(1);
    expect(res.places[0].name).toBe('天坛');
  });

  it('请求路径与参数正确', async () => {
    mockFetch(200, { places: [] });
    const api = createApiClient('http://localhost:7800');
    await api.search('故宫', '北京');
    const [url] = (globalThis.fetch as jest.Mock).mock.calls[0] as [string];
    expect(url).toBe('http://localhost:7800/search?q=%E6%95%85%E5%AE%AB&city=%E5%8C%97%E4%BA%AC');
  });

  it('非 2xx 归一为 ApiError，带后端 message', async () => {
    mockFetch(400, { error: '出行方式无效: "fly"(支持 driving/walking/transit)' });
    const api = createApiClient('http://localhost:7800');
    await expect(api.plan({} as never)).rejects.toMatchObject({
      status: 400,
      message: expect.stringContaining('出行方式无效'),
    });
  });

  it('非 JSON 错误体也能归一', async () => {
    globalThis.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 502,
      json: () => Promise.reject(new Error('not json')),
      text: () => Promise.resolve('<html>bad gateway</html>'),
    }) as unknown as typeof fetch;
    const api = createApiClient('http://localhost:7800');
    await expect(api.search('x')).rejects.toBeInstanceOf(ApiError);
  });

  it('超时抛 ApiError(status=0)', async () => {
    jest.useFakeTimers();
    globalThis.fetch = jest.fn(
      (_url: string, init?: RequestInit) =>
        new Promise((_resolve, reject) => {
          const abortErr = new Error('Aborted');
          abortErr.name = 'AbortError';
          init?.signal?.addEventListener('abort', () => reject(abortErr));
        }),
    ) as unknown as typeof fetch;
    const api = createApiClient('http://localhost:7800', 50);
    const p = api.search('x');
    jest.advanceTimersByTime(60);
    await expect(p).rejects.toMatchObject({ status: 0 });
    jest.useRealTimers();
  });

  it('网络错误归一为 ApiError(status=0)', async () => {
    globalThis.fetch = jest.fn().mockRejectedValue(new TypeError('Network request failed'));
    const api = createApiClient('http://localhost:7800');
    await expect(api.search('x')).rejects.toMatchObject({ status: 0 });
  });

  it('plan 请求体字段名与后端 wire contract 一致', async () => {
    mockFetch(200, { order: [], order_idx: [], total_km: 0, warnings: [], degraded: [], is_degraded: false });
    const api = createApiClient('http://localhost:7800');
    await api.plan({
      origin: { name: '天安门', lat: 39.9087, lng: 116.3975 },
      destinations: [{ name: '天坛', lat: 39.8822, lng: 116.4066 }],
      mode: 'walking',
      manual: false,
      segments: ['walking'],
    });
    const [, init] = (globalThis.fetch as jest.Mock).mock.calls[0] as [string, RequestInit];
    const body = JSON.parse(String(init.body));
    expect(Object.keys(body).sort()).toEqual(['destinations', 'manual', 'mode', 'origin', 'segments']);
    expect(body.origin).toEqual({ name: '天安门', lat: 39.9087, lng: 116.3975 });
  });
});
