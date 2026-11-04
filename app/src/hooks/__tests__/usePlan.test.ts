import { act, renderHook } from '@testing-library/react-native';
import { createApiClient, type ApiClient } from '../../api/client';
import { usePlan, type PlanInput } from '../usePlan';

jest.mock('../../api/client', () => {
  const actual = jest.requireActual('../../api/client');
  return { ...actual, createApiClient: jest.fn() };
});

const mockApi = createApiClient as jest.MockedFunction<typeof createApiClient>;

function makeApi(overrides?: Partial<ApiClient>) {
  return {
    plan: jest.fn().mockResolvedValue({
      order: ['天安门', '天坛', '故宫'],
      order_idx: [0, 2, 1],
      total_km: 12.3,
      warnings: [],
      degraded: [],
      is_degraded: false,
    }),
    route: jest.fn().mockResolvedValue({ polyline: [[116.4, 39.9]] }),
    search: jest.fn(),
    ...(overrides as object),
  } as unknown as jest.Mocked<ApiClient>;
}

const input: PlanInput = {
  origin: { id: 'p0', name: '天安门', lat: 39.9087, lng: 116.3975 },
  destinations: [
    { id: 'p1', name: '故宫', lat: 39.9163, lng: 116.3972 },
    { id: 'p2', name: '天坛', lat: 39.8822, lng: 116.4066 },
  ],
  mode: 'driving',
  manual: false,
};

describe('usePlan', () => {
  beforeEach(() => {
    mockApi.mockReset();
  });

  it('plan 成功：请求体正确，按 order_idx 逐段拉真实路网', async () => {
    const api = makeApi();
    mockApi.mockReturnValue(api);
    const { result } = await renderHook(() => usePlan());
    await act(async () => {
      await result.current.plan(input);
    });
    expect(result.current.result?.total_km).toBe(12.3);
    expect(result.current.planError).toBeNull();
    expect(result.current.planning).toBe(false);
    // order_idx=[0,2,1] → 顺序：天安门(origin) → 天坛 → 故宫 → 共两段
    expect(api.plan).toHaveBeenCalledWith(
      expect.objectContaining({ mode: 'driving', manual: false }),
    );
    expect(api.route).toHaveBeenCalledTimes(2);
    // 第一段：天安门→天坛（lng,lat 顺序，与后端 parsePoint 契约一致）
    expect(api.route).toHaveBeenNthCalledWith(1, '116.3975,39.9087', '116.4066,39.8822', 'driving');
    expect(result.current.segments).toHaveLength(2);
  });

  it('某段 route 失败 → 该段退化为直线，不影响整体', async () => {
    const api = makeApi();
    api.route = jest
      .fn()
      .mockRejectedValueOnce(new Error('路线获取失败'))
      .mockResolvedValueOnce({ polyline: [[116.4, 39.9]] });
    mockApi.mockReturnValue(api);
    const { result } = await renderHook(() => usePlan());
    await act(async () => {
      await result.current.plan(input);
    });
    expect(result.current.segments).toHaveLength(2);
    expect(result.current.segments[0].points).toEqual([
      [116.3975, 39.9087],
      [116.4066, 39.8822],
    ]); // 直线 = 起终点两点
    expect(result.current.segments[1].points).toEqual([[116.4, 39.9]]);
    expect(result.current.planError).toBeNull();
  });

  it('混合出行：segments 传入请求体，逐段 route 用该段方式', async () => {
    const api = makeApi();
    mockApi.mockReturnValue(api);
    const { result } = await renderHook(() => usePlan());
    await act(async () => {
      await result.current.plan({ ...input, segments: ['walking', 'transit'] });
    });
    expect(api.plan).toHaveBeenCalledWith(
      expect.objectContaining({ mode: 'driving', manual: false, segments: ['walking', 'transit'] }),
    );
    // 顺序：天安门→天坛→故宫。段1 = walking，段2 = transit
    expect(api.route).toHaveBeenNthCalledWith(1, '116.3975,39.9087', '116.4066,39.8822', 'walking');
    expect(api.route).toHaveBeenNthCalledWith(2, '116.4066,39.8822', '116.3972,39.9163', 'transit');
    expect(result.current.segments[0].mode).toBe('walking');
    expect(result.current.segments[1].mode).toBe('transit');
  });

  it('全段与全局 mode 相同 → 请求体不带 segments（保住驾车批量快路径）', async () => {
    const api = makeApi();
    mockApi.mockReturnValue(api);
    const { result } = await renderHook(() => usePlan());
    await act(async () => {
      await result.current.plan({ ...input, segments: ['driving', 'driving'] });
    });
    const body = (api.plan as jest.Mock).mock.calls[0][0] as Record<string, unknown>;
    expect(body.segments).toBeUndefined();
    expect(result.current.planError).toBeNull();
  });

  it('plan 失败 → planError 带后端 message，result 为空', async () => {
    const api = makeApi();
    api.plan = jest.fn().mockRejectedValue(Object.assign(new Error('出行方式无效'), { status: 400 }));
    mockApi.mockReturnValue(api);
    const { result } = await renderHook(() => usePlan());
    await act(async () => {
      await result.current.plan(input);
    });
    expect(result.current.planError).toBe('出行方式无效');
    expect(result.current.result).toBeNull();
  });

  it('少于 2 个地点 → 不发请求，提示但不报错', async () => {
    const api = makeApi();
    mockApi.mockReturnValue(api);
    const { result } = await renderHook(() => usePlan());
    await act(async () => {
      await result.current.plan({ ...input, destinations: [] });
    });
    expect(api.plan).not.toHaveBeenCalled();
    expect(result.current.planError).toBe('先加地点：至少 2 个才能规划（起点 + 至少 1 个途经点）');
    expect(result.current.planning).toBe(false);
  });

  it('clear 清空结果', async () => {
    const api = makeApi();
    mockApi.mockReturnValue(api);
    const { result } = await renderHook(() => usePlan());
    await act(async () => {
      await result.current.plan(input);
    });
    expect(result.current.result).not.toBeNull();
    await act(async () => {
      result.current.clear();
    });
    expect(result.current.result).toBeNull();
    expect(result.current.segments).toEqual([]);
    expect(result.current.planError).toBeNull();
  });
});

