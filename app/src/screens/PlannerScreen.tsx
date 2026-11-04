import { useCallback, useEffect, useRef, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { ModeSelector } from '../components/ModeSelector';
import { PlanResult } from '../components/PlanResult';
import { PointList } from '../components/PointList';
import { SearchBar } from '../components/SearchBar';
import { WarningBar } from '../components/WarningBar';
import { API_BASE_URL } from '../config';
import { usePlan } from '../hooks/usePlan';
import { usePoints, useSearch } from '../hooks/useSearch';
import type { Mode } from '../api/types';
import { MapWebView, type MapWebViewHandle } from '../map/MapWebView';
import { colors } from '../theme/tokens';

/**
 * 一期主屏：左面板（搜索 + 点列表 + 模式 + 规划结果）+ 右地图。
 * 数据流单向：hooks 持状态 → 组件渲染 → effect 把 points/segments 投影到地图。
 * 规划按钮永不禁用：条件不满足时点击给出可行动的提示（禁用按钮会让用户以为功能坏了）。
 */
export function PlannerScreen() {
  const mapRef = useRef<MapWebViewHandle>(null);
  const search = useSearch();
  const { points, add, remove, move, setLegMode } = usePoints();
  const { planning, planError, result, segments, plan, clear } = usePlan();
  const [ready, setReady] = useState(false);
  const [mode, setMode] = useState<Mode>('driving');
  const [manual, setManual] = useState(false);

  // points → 地图投影：全量重画（点数 ≤50，全量比 diff 简单且不会漂移）
  useEffect(() => {
    if (!ready) return;
    const map = mapRef.current;
    if (!map) return;
    map.post({ type: 'clearMarkers' });
    points.forEach((p, i) => {
      map.post({
        type: 'addMarker',
        id: p.id,
        lat: p.lat,
        lng: p.lng,
        title: p.name,
        label: String(i + 1),
      });
    });
    map.post({ type: 'fitBounds' });
  }, [points, ready]);

  // 规划结果 → 画线投影
  useEffect(() => {
    if (!ready) return;
    const map = mapRef.current;
    if (!map) return;
    map.post({ type: 'clearPolylines' });
    if (segments.length === 0) return;
    map.post({ type: 'drawPolyline', segments });
    map.post({ type: 'fitBounds' });
  }, [segments, ready]);

  const handleMapTap = useCallback(
    (lat: number, lng: number) => {
      add({ name: coordName(lat, lng), lat, lng });
    },
    [add],
  );

  const handlePick = useCallback(
    (place: { name: string; lat: number; lng: number }) => {
      add({ name: place.name, lat: place.lat, lng: place.lng });
      search.setQuery('');
      mapRef.current?.post({ type: 'setCenter', lat: place.lat, lng: place.lng, zoom: 14 });
    },
    [add, search],
  );

  const handlePlan = useCallback(() => {
    if (points.length < 2) return; // 提示由 planError 承载（plan() 内部会设置）
    // legMode 挂在点上：段 i 的方式 = 点 i 的 legMode（缺省跟全局）。
    // 全段与全局 mode 相同时的 segments 去重由 usePlan 统一处理
    const segs = points.slice(0, -1).map((p) => p.legMode ?? mode);
    plan({ origin: points[0], destinations: points.slice(1), mode, manual, segments: segs });
  }, [points, mode, manual, plan]);

  const onMessage = useCallback(
    (msg: { type: string; lat?: number; lng?: number }) => {
      if (msg.type === 'ready') {
        setReady(true);
      } else if (msg.type === 'mapTap' && typeof msg.lat === 'number' && typeof msg.lng === 'number') {
        handleMapTap(msg.lat, msg.lng);
      }
    },
    [handleMapTap],
  );

  return (
    <SafeAreaView style={styles.safe}>
      <View style={styles.topbar}>
        <Text style={styles.title}>路线规划</Text>
        <Text style={styles.subtitle}>{ready ? '' : '地图加载中…'}</Text>
      </View>
      <View style={styles.body}>
        <ScrollView style={styles.panel} contentContainerStyle={styles.panelContent}>
          <SearchBar
            query={search.query}
            onQueryChange={search.setQuery}
            city={search.city}
            onCityChange={search.setCity}
            results={search.results}
            loading={search.loading}
            error={search.error}
            onPick={handlePick}
          />
          <View style={styles.divider} />
          <Text style={styles.sectionTitle}>地点（{points.length}）</Text>
          <PointList points={points} onRemove={remove} onMove={move} onLegModeChange={setLegMode} />
          <View style={styles.divider} />
          <ModeSelector mode={mode} onModeChange={setMode} manual={manual} onManualChange={setManual} />
          <Pressable style={[styles.planBtn, planning && styles.planBtnBusy]} onPress={handlePlan}>
            <Text style={styles.planBtnText}>{planning ? '规划中…' : '开始规划'}</Text>
          </Pressable>
          {planError ? <Text style={styles.planError}>{planError}</Text> : null}
          {result ? (
            <>
              <WarningBar warnings={result.warnings} />
              <PlanResult
                result={result}
                points={points}
                orderIdx={result.order_idx}
                segmentModes={segments.map((s) => s.mode)}
              />
              <Pressable onPress={clear}>
                <Text style={styles.clearText}>清除规划</Text>
              </Pressable>
            </>
          ) : null}
        </ScrollView>
        <View style={styles.mapWrap}>
          <MapWebView ref={mapRef} onMessage={onMessage} />
        </View>
      </View>
    </SafeAreaView>
  );
}

function coordName(lat: number, lng: number): string {
  return `${lat.toFixed(5)}, ${lng.toFixed(5)}`;
}

const styles = StyleSheet.create({
  safe: { flex: 1, backgroundColor: colors.bgPage },
  topbar: {
    height: 56,
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 12,
    backgroundColor: colors.bgSurface,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.border,
  },
  title: { fontSize: 17, fontWeight: '600', color: colors.ink },
  subtitle: { marginLeft: 'auto', fontSize: 11, color: colors.textTertiary },
  body: { flex: 1, flexDirection: 'row' },
  panel: {
    width: 320,
    backgroundColor: colors.bgSurface,
    borderRightWidth: StyleSheet.hairlineWidth,
    borderRightColor: colors.border,
  },
  panelContent: { padding: 12, gap: 12 },
  divider: { height: StyleSheet.hairlineWidth, backgroundColor: colors.borderSoft },
  sectionTitle: { fontSize: 13, fontWeight: '600', color: colors.ink },
  planBtn: {
    height: 42,
    borderRadius: 10,
    backgroundColor: colors.ink,
    alignItems: 'center',
    justifyContent: 'center',
  },
  planBtnBusy: { opacity: 0.6 },
  planBtnText: { fontSize: 15, fontWeight: '600', color: '#FFFFFF' },
  planError: { fontSize: 12, color: colors.danger, lineHeight: 17 },
  clearText: { fontSize: 12, color: colors.textSecondary, textAlign: 'center' },
  mapWrap: { flex: 1 },
});
