import { decodeWebViewMessage, encodeToWebView, type BridgeMessage } from '../bridge';

describe('map bridge protocol', () => {
  describe('RN → WebView 命令', () => {
    it('addMarker 编码带 type 与字段', () => {
      const json = encodeToWebView({ type: 'addMarker', id: 'p0', lat: 39.9, lng: 116.4, title: '天安门' });
      const obj = JSON.parse(json);
      expect(obj).toEqual({ type: 'addMarker', id: 'p0', lat: 39.9, lng: 116.4, title: '天安门' });
    });

    it('drawPolyline 编码 segments', () => {
      const json = encodeToWebView({
        type: 'drawPolyline',
        segments: [{ mode: 'driving', points: [[116.397, 39.908], [116.406, 39.882]] }],
      });
      const obj = JSON.parse(json);
      expect(obj.type).toBe('drawPolyline');
      expect(obj.segments[0].mode).toBe('driving');
    });

    it('encode 输出必须是合法 JSON（WebView 侧 postMessage 只吃字符串）', () => {
      const json = encodeToWebView({ type: 'fitBounds' });
      expect(() => JSON.parse(json)).not.toThrow();
    });
  });

  describe('WebView → RN 事件', () => {
    it('解码 markerDrag', () => {
      const msg = decodeWebViewMessage(
        JSON.stringify({ type: 'markerDrag', id: 'p1', lat: 39.88, lng: 116.4 }),
      );
      expect(msg).toEqual({ type: 'markerDrag', id: 'p1', lat: 39.88, lng: 116.4 });
    });

    it('解码 mapTap', () => {
      const msg = decodeWebViewMessage(JSON.stringify({ type: 'mapTap', lat: 1, lng: 2 }));
      expect(msg).toEqual({ type: 'mapTap', lat: 1, lng: 2 });
    });

    it('坏消息返回 null，绝不抛错（WebView 侧内容不可信）', () => {
      expect(decodeWebViewMessage('not json')).toBeNull();
      expect(decodeWebViewMessage('{"type":"unknown"}')).toBeNull();
      expect(decodeWebViewMessage('{"type":"mapTap"}')).toBeNull(); // 缺字段
      expect(decodeWebViewMessage('[]')).toBeNull();
    });
  });

  it('BridgeMessage 类型收窄：未知 type 不在联合里', () => {
    const bad: { type: string } = { type: 'nope' };
    // @ts-expect-error 未知 type 编译期就该被拦住
    const m: BridgeMessage = bad;
    expect(m).toBeDefined();
  });
});
