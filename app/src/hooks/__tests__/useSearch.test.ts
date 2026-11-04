import { act, renderHook, waitFor } from '@testing-library/react-native';
import { createApiClient, type ApiClient } from '../../api/client';
import { useSearch } from '../useSearch';

jest.mock('../../api/client', () => {
  const actual = jest.requireActual('../../api/client');
  return { ...actual, createApiClient: jest.fn() };
});

const mockApi = createApiClient as jest.MockedFunction<typeof createApiClient>;

function fakeApi() {
  return {
    search: jest.fn().mockResolvedValue({
      places: [{ name: '天坛', address: '北京市东城区', lat: 39.8822, lng: 116.4066 }],
    }),
    plan: jest.fn(),
  } as unknown as ApiClient;
}

/** 在 act 内推进假时钟并冲刷微任务：防抖回调里的 await 能在断言前完成 */
async function advance(ms: number) {
  await act(async () => {
    jest.advanceTimersByTime(ms);
    await Promise.resolve();
  });
}

describe('useSearch', () => {
  beforeEach(() => {
    jest.useFakeTimers();
    mockApi.mockReturnValue(fakeApi());
  });
  afterEach(() => {
    jest.useRealTimers();
  });

  it('输入后防抖 300ms 才请求', async () => {
    const { result } = await renderHook(() => useSearch());
    await act(async () => {
      result.current.setQuery('天坛');
      await Promise.resolve();
    });
    expect(result.current.loading).toBe(true); // 已进加载态但还没发请求
    const api = mockApi.mock.results[0].value as unknown as { search: jest.Mock };
    expect(api.search).not.toHaveBeenCalled();

    await advance(300);
    await waitFor(() => expect(result.current.results).toHaveLength(1));
    expect(result.current.loading).toBe(false);
  });

  it('连续输入只发最后一次请求', async () => {
    const { result } = await renderHook(() => useSearch());
    await act(async () => {
      result.current.setQuery('天');
      jest.advanceTimersByTime(200); // 不足防抖窗口
      result.current.setQuery('天坛');
      await advance(300);
    });
    await waitFor(() => expect(result.current.results).toHaveLength(1));
    const api = mockApi.mock.results[0].value as unknown as { search: jest.Mock };
    expect(api.search).toHaveBeenCalledTimes(1);
    expect(api.search).toHaveBeenCalledWith('天坛', undefined);
  });

  it('空输入清空结果不发请求', async () => {
    const { result } = await renderHook(() => useSearch());
    await act(async () => {
      result.current.setQuery('天坛');
    });
    await advance(300);
    await waitFor(() => expect(result.current.results).toHaveLength(1));
    await act(async () => {
      result.current.setQuery('');
      await Promise.resolve();
    });
    expect(result.current.results).toEqual([]);
    const api = mockApi.mock.results[0].value as unknown as { search: jest.Mock };
    expect(api.search).toHaveBeenCalledTimes(1); // 没有第二次
  });

  it('错误进 error 状态而不是抛出', async () => {
    const api = fakeApi();
    api.search = jest.fn().mockRejectedValue(Object.assign(new Error('网络错误'), { status: 0 }));
    mockApi.mockReturnValue(api);
    const { result } = await renderHook(() => useSearch());
    await act(async () => {
      result.current.setQuery('天坛');
    });
    await advance(300);
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(result.current.results).toEqual([]);
  });
});
