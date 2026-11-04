import { useCallback, useRef, useState } from 'react';

import { createApiClient, type ApiClient } from '../api/client';
import type { Mode, PlanResult } from '../api/types';
import { API_BASE_URL } from '../config';
import type { RnPoint } from './pointsLogic';

export interface PlanInput {
  origin: RnPoint;
  destinations: RnPoint[];
  mode: Mode;
  manual: boolean;
  /**
   * 混合出行：每段方式（长度 = 点数-1，与后端 Validate 契约一致）。
   * 全段与 mode 相同时必须不传 —— 后端对 segments 非空的请求走逐对路径（上限 10 点），
   * 空着才保得住驾车批量快路径。
   */
  segments?: Mode[];
}

/** 一段画线数据：mode 决定颜色；points 为 [lng,lat]（与后端 polyline 一致） */
export interface PlanSegment {
  mode: Mode;
  points: Array<[number, number]>;
}

const MIN_POINTS_HINT = '先加地点：至少 2 个才能规划（起点 + 至少 1 个途经点）';

/**
 * 规划状态机：plan() → 请求 /plan → 成功后逐段拉 /route 画真实路网；
 * 某段路线拉不到就退化为该段起终点的直线（与旧前端语义一致）。
 * seq 守卫过期响应（连点两次规划按钮时，旧结果不许覆盖新结果）。
 */
export function usePlan(apiInput?: ApiClient) {
  const apiRef = useRef<ApiClient | null>(null);
  if (apiInput) apiRef.current = apiInput;
  else if (!apiRef.current) apiRef.current = createApiClient(API_BASE_URL);
  const api = apiRef.current;

  const [planning, setPlanning] = useState(false);
  const [planError, setPlanError] = useState<string | null>(null);
  const [result, setResult] = useState<PlanResult | null>(null);
  const [segments, setSegments] = useState<PlanSegment[]>([]);
  const seqRef = useRef(0);

  const clear = useCallback(() => {
    seqRef.current++;
    setResult(null);
    setSegments([]);
    setPlanError(null);
    setPlanning(false);
  }, []);

  const plan = useCallback(
    async (input: PlanInput) => {
      const seq = ++seqRef.current;
      if (input.destinations.length === 0) {
        setPlanError(MIN_POINTS_HINT);
        setResult(null);
        setSegments([]);
        return;
      }
      setPlanning(true);
      setPlanError(null);
      setSegments([]);
      try {
        // 全段与 mode 相同时不传 segments：后端对非空 segments 走逐对路径（上限 10 点），
        // 空着才保得住驾车批量快路径。判断收在 usePlan（所有调用方必经），调用方不用各自记得
        const legModes = input.segments;
        const hasCustomSegs =
          !!legModes && legModes.length > 0 && legModes.some((m) => m !== input.mode);
        const res = await api.plan({
          origin: { name: input.origin.name, lat: input.origin.lat, lng: input.origin.lng },
          destinations: input.destinations.map((p) => ({ name: p.name, lat: p.lat, lng: p.lng })),
          mode: input.mode,
          manual: input.manual,
          segments: hasCustomSegs ? legModes : undefined,
        });
        if (seq !== seqRef.current) return;
        setResult(res);

        // order_idx 给机器用：以 0 开头的全排列，索引含起点的完整点列表（origin=0，
        // 后端 planner_test 钉住该契约）。按它把点串成访问顺序，再逐段拉真实路网
        const all: RnPoint[] = [input.origin, ...input.destinations];
        const ordered: RnPoint[] = res.order_idx.map((i) => {
          const p = all[i];
          if (!p) throw new Error(`order_idx 越界: ${i}（共 ${all.length} 个点）`);
          return p;
        });
        const segs: PlanSegment[] = [];
        for (let i = 0; i < ordered.length - 1; i++) {
          const a = ordered[i];
          const b = ordered[i + 1];
          const legMode = input.segments?.[i] ?? input.mode;
          // 直线兜底：route 失败时用起终点两点
          let seg: PlanSegment = { mode: legMode, points: [[a.lng, a.lat], [b.lng, b.lat]] };
          try {
            const r = await api.route(`${a.lng},${a.lat}`, `${b.lng},${b.lat}`, legMode);
            if (seq !== seqRef.current) return;
            if (r.polyline.length > 0) seg = { mode: legMode, points: r.polyline };
          } catch {
            if (seq !== seqRef.current) return;
            // 画线失败不算规划失败：直线兜底，warnings 由 /plan 的降级信息承担
          }
          segs.push(seg);
          if (seq === seqRef.current) setSegments([...segs]); // 逐段上屏，长路线不用干等
        }
      } catch (e) {
        if (seq !== seqRef.current) return;
        setPlanError(e instanceof Error ? e.message : String(e));
        setResult(null);
        setSegments([]);
      } finally {
        if (seq === seqRef.current) setPlanning(false);
      }
    },
    [api],
  );

  return { planning, planError, result, segments, plan, clear };
}
