import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, type ReactNode } from 'react';
import { StyleSheet } from 'react-native';

import { decodeWebViewMessage, encodeToWebView, type BridgeCommand, type BridgeMessage } from './bridge';
import { leafletHtml } from './leafletHtml';

/**
 * react-native-webview 的类型定义与 React 19 / RN 0.88 类型不兼容
 * （组件 props 被解析成 never），这里手动声明本组件实际用到的 props 子集，
 * 并对模块做一次类型适配。webview 包升级修复后可删掉这个适配层。
 */
interface WebViewProps {
  source: { html: string };
  style?: object;
  onMessage?: (e: { nativeEvent: { data: string } }) => void;
  originWhitelist?: string[];
  javaScriptEnabled?: boolean;
  domStorageEnabled?: boolean;
  setSupportMultipleWindows?: boolean;
  cacheEnabled?: boolean;
  children?: ReactNode;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const WebView = require('react-native-webview').WebView as any;

export interface MapWebViewHandle {
  /** 向地图发送命令（addMarker / drawPolyline / fitBounds…） */
  post(cmd: BridgeCommand): void;
}

interface Props {
  onMessage?: (msg: BridgeMessage) => void;
}

/**
 * Leaflet 地图容器。
 * WebView 加载内联 leafletHtml；RN 侧通过 injectJavaScript 发命令，
 * 地图侧通过 postMessage 回事件（见 bridge.ts 协议）。
 */
export const MapWebView = forwardRef<MapWebViewHandle, Props>(function MapWebView({ onMessage }, ref) {
  const webRef = useRef<any>(null);
  // ready 前到达的命令先缓存：WebView 尚未挂载时 injectJavaScript 会静默丢失
  const queueRef = useRef<BridgeCommand[]>([]);
  const readyRef = useRef(false);
  const onMessageRef = useRef(onMessage);
  onMessageRef.current = onMessage;

  const flush = useCallback(() => {
    const q = queueRef.current;
    queueRef.current = [];
    q.forEach((cmd) => {
      webRef.current?.injectJavaScript(`window.__handleCmd(${encodeToWebView(cmd)}); true;`);
    });
  }, []);

  useImperativeHandle(ref, () => ({
    post(cmd: BridgeCommand) {
      if (!readyRef.current) {
        queueRef.current.push(cmd);
        return;
      }
      webRef.current?.injectJavaScript(`window.__handleCmd(${encodeToWebView(cmd)}); true;`);
    },
  }));

  const handleMessage = useCallback(
    (e: { nativeEvent: { data: string } }) => {
      let data = e.nativeEvent.data;
      // Android WebView 会把 message 包一层；剥掉外层再解析
      try {
        const outer = JSON.parse(data);
        if (outer && typeof outer === 'object' && 'data' in outer && typeof outer.data === 'string') {
          data = outer.data;
        }
      } catch {
        // 不是包裹消息，按原文处理
      }
      if (data.includes('"ready"')) {
        readyRef.current = true;
        flush();
        return;
      }
      const msg = decodeWebViewMessage(data);
      if (msg) onMessageRef.current?.(msg);
    },
    [flush],
  );

  useEffect(() => () => {
    readyRef.current = false;
    queueRef.current = [];
  }, []);

  return (
    <WebView
      ref={webRef}
      source={{ html: leafletHtml }}
      style={styles.map}
      onMessage={handleMessage}
      originWhitelist={['*']}
      javaScriptEnabled
      domStorageEnabled
      setSupportMultipleWindows={false}
      // 瓦片来自网络，缓存能显著提速
      cacheEnabled
    />
  );
});

const styles = StyleSheet.create({
  map: { flex: 1 },
});
