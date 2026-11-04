/**
 * 地图桥协议 —— RN(React Native) 与 Leaflet(WebView) 之间的全部通信契约。
 *
 * 两侧只通过 JSON 字符串交流（postMessage），所有消息带 type 字段。
 * 类型只在本文件定义：改协议 = 改这里 + leaflet.html 两侧同步。
 */

// —— RN → WebView：命令 ——

export interface AddMarkerMsg {
  type: 'addMarker';
  id: string;
  lat: number;
  lng: number;
  title?: string;
  /** 序号徽标文案（如 "1"）；有值时地图用编号圆点替代默认图钉 */
  label?: string;
  draggable?: boolean;
}

export interface RemoveMarkerMsg {
  type: 'removeMarker';
  id: string;
}

export interface ClearMarkersMsg {
  type: 'clearMarkers';
}

export interface DrawPolylineMsg {
  type: 'drawPolyline';
  segments: Array<{
    mode: string;
    /** [lng, lat] 数组 —— 与后端 polyline 一致，经度在前 */
    points: Array<[number, number]>;
  }>;
}

export interface ClearPolylinesMsg {
  type: 'clearPolylines';
}

export interface FitBoundsMsg {
  type: 'fitBounds';
}

export interface SetCenterMsg {
  type: 'setCenter';
  lat: number;
  lng: number;
  zoom?: number;
}

export type BridgeCommand =
  | AddMarkerMsg
  | RemoveMarkerMsg
  | ClearMarkersMsg
  | DrawPolylineMsg
  | ClearPolylinesMsg
  | FitBoundsMsg
  | SetCenterMsg;

// —— WebView → RN：事件 ——

export interface MarkerDragMsg {
  type: 'markerDrag';
  id: string;
  lat: number;
  lng: number;
}

export interface MapTapMsg {
  type: 'mapTap';
  lat: number;
  lng: number;
}

export type BridgeMessage = MarkerDragMsg | MapTapMsg;

/** RN 侧调用：命令序列化为 JSON 字符串发给 WebView（injectJavaScript 消费）。 */
export function encodeToWebView(cmd: BridgeCommand): string {
  return JSON.stringify(cmd);
}

/**
 * RN 侧调用：解析 WebView 发来的消息。
 * 任何解析失败/未知 type/缺字段都返回 null —— WebView 侧内容不可信，绝不抛错。
 */
export function decodeWebViewMessage(raw: string): BridgeMessage | null {
  let obj: unknown;
  try {
    obj = JSON.parse(raw);
  } catch {
    return null;
  }
  if (typeof obj !== 'object' || obj === null) return null;
  const o = obj as Record<string, unknown>;

  if (o.type === 'markerDrag' && isFiniteCoord(o.lat) && isFiniteCoord(o.lng)) {
    return { type: 'markerDrag', id: String(o.id), lat: Number(o.lat), lng: Number(o.lng) };
  }
  if (o.type === 'mapTap' && isFiniteCoord(o.lat) && isFiniteCoord(o.lng)) {
    return { type: 'mapTap', lat: Number(o.lat), lng: Number(o.lng) };
  }
  return null;
}

function isFiniteCoord(v: unknown): boolean {
  return typeof v === 'number' && Number.isFinite(v);
}
