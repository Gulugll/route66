/**
 * 后端 wire contract 的 TS 镜像。
 * 字段名与 internal/planner/planner.go、internal/model/model.go 的 json tag 一字不差，
 * 由 router_test.go（Go 侧）与本目录测试（TS 侧）双向钉住。
 */

export interface Point {
  name: string;
  lat: number;
  lng: number;
}

export interface Place {
  name: string;
  address: string;
  lat: number;
  lng: number;
}

export type Mode = 'driving' | 'walking' | 'transit';

export interface PlanRequest {
  origin: Point;
  destinations: Point[];
  mode: Mode;
  /** true = 按列表顺序，不跑 TSP */
  manual: boolean;
  /** 可选：每段的方式，如 ['walking','transit'] */
  segments?: string[];
}

export interface PlanResult {
  order: string[];
  order_idx: number[];
  total_km: number;
  warnings: string[];
  degraded: string[];
  is_degraded: boolean;
}

export interface SearchResponse {
  places: Place[];
}

/** GET /route 响应：[lng,lat] 数组；空数组 = 无轨迹，调用方画直线 */
export interface RouteResponse {
  polyline: Array<[number, number]>;
}
