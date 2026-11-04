/**
 * 点列表的纯逻辑（不依赖 React，便于 TDD）。
 * legMode（点到下一点怎么走）挂在点上，与后端 /plan 的 segments 语义对齐；
 * 最后一个点没有"下一点"，它的 legMode 无意义。
 */

import type { Mode } from '../api/types';

export interface RnPoint {
  id: string;
  name: string;
  lat: number;
  lng: number;
  /** 本点到下一点的方式；缺省 = 跟随全局 mode */
  legMode?: Mode;
}

export function addPoint(points: RnPoint[], p: RnPoint): RnPoint[] {
  return [...points, p];
}

export function removePoint(points: RnPoint[], id: string): RnPoint[] {
  return points.filter((p) => p.id !== id);
}

/** dir: -1 上移 / +1 下移；越界时原样返回（夹住边界） */
export function movePoint(points: RnPoint[], id: string, dir: -1 | 1): RnPoint[] {
  const i = points.findIndex((p) => p.id === id);
  const j = i + dir;
  if (i < 0 || j < 0 || j >= points.length) return points;
  const out = [...points];
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}

/** id 形如 p0/p1/…：取现有最大数字 +1，删除中间点后不会撞 id */
export function nextPointId(points: RnPoint[]): string {
  let max = -1;
  for (const p of points) {
    const m = /^p(\d+)$/.exec(p.id);
    if (m) max = Math.max(max, Number(m[1]));
  }
  return `p${max + 1}`;
}
