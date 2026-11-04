import { Pressable, StyleSheet, Text, View } from 'react-native';

import type { Mode } from '../api/types';
import type { RnPoint } from '../hooks/pointsLogic';
import { colors, modeColors, modeLabels, radius, spacing } from '../theme/tokens';

interface Props {
  points: RnPoint[];
  onRemove: (id: string) => void;
  onMove: (id: string, dir: -1 | 1) => void;
  onLegModeChange: (id: string, m: Mode) => void;
}

/**
 * 点列表：序号 + 名称 + 上移/下移/删除 + 段方式（混合出行）。
 * legMode 挂在点上（点到下一点怎么走），最后一个点没有"下一点"不显示。
 * 空态给可行动的引导。
 */
export function PointList({ points, onRemove, onMove, onLegModeChange }: Props) {
  if (points.length === 0) {
    return (
      <Text style={styles.empty}>
        还没有地点：搜索添加，或直接点地图落点。至少 2 个才能规划。
      </Text>
    );
  }
  return (
    <View style={styles.wrap}>
      {points.map((p, i) => (
        <View key={p.id} style={styles.row}>
          <Text style={styles.index}>{i + 1}</Text>
          <View style={styles.info}>
            <Text style={styles.name} numberOfLines={1}>
              {p.name}
            </Text>
            <Text style={styles.coord}>
              {p.lat.toFixed(5)}, {p.lng.toFixed(5)}
            </Text>
            {i < points.length - 1 ? (
              <View style={styles.legRow}>
                <Text style={styles.legLabel}>到下一点</Text>
                {modeLabels.map((m) => {
                  const active = (p.legMode ?? 'driving') === m.value;
                  return (
                    <Pressable
                      key={m.value}
                      style={[styles.legChip, active && { backgroundColor: modeColors[m.value] }]}
                      onPress={() => onLegModeChange(p.id, m.value as Mode)}
                    >
                      <Text style={[styles.legChipText, active && styles.legChipTextActive]}>
                        {m.label}
                      </Text>
                    </Pressable>
                  );
                })}
              </View>
            ) : null}
          </View>
          <View style={styles.btns}>
            <Pressable
              style={[styles.btn, i === 0 && styles.btnDisabled]}
              disabled={i === 0}
              onPress={() => onMove(p.id, -1)}
            >
              <Text style={styles.btnText}>↑</Text>
            </Pressable>
            <Pressable
              style={[styles.btn, i === points.length - 1 && styles.btnDisabled]}
              disabled={i === points.length - 1}
              onPress={() => onMove(p.id, 1)}
            >
              <Text style={styles.btnText}>↓</Text>
            </Pressable>
            <Pressable style={[styles.btn, styles.removeBtn]} onPress={() => onRemove(p.id)}>
              <Text style={[styles.btnText, styles.removeText]}>✕</Text>
            </Pressable>
          </View>
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: spacing.sm },
  empty: { fontSize: 12, color: colors.textSecondary, lineHeight: 18 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    borderRadius: radius.chip,
    borderWidth: 1,
    borderColor: colors.borderSoft,
    backgroundColor: colors.bgSurface,
    paddingHorizontal: 8,
    paddingVertical: 6,
  },
  index: {
    width: 20,
    height: 20,
    lineHeight: 20,
    textAlign: 'center',
    borderRadius: 10,
    backgroundColor: colors.ink,
    color: '#FFFFFF',
    fontSize: 11,
    fontWeight: '600',
    overflow: 'hidden',
  },
  info: { flex: 1 },
  name: { fontSize: 14, color: colors.ink, fontWeight: '500' },
  coord: { fontSize: 10, color: colors.textTertiary },
  legRow: { flexDirection: 'row', alignItems: 'center', gap: 4, marginTop: 4 },
  legLabel: { fontSize: 10, color: colors.textTertiary, marginRight: 2 },
  legChip: {
    width: 26,
    height: 20,
    borderRadius: 6,
    backgroundColor: colors.bgSunken,
    alignItems: 'center',
    justifyContent: 'center',
  },
  legChipText: { fontSize: 11, opacity: 0.55 },
  legChipTextActive: { opacity: 1 },
  btns: { flexDirection: 'row', gap: 4 },
  btn: {
    width: 26,
    height: 26,
    borderRadius: 6,
    backgroundColor: colors.bgSunken,
    alignItems: 'center',
    justifyContent: 'center',
  },
  btnDisabled: { opacity: 0.3 },
  btnText: { fontSize: 13, color: colors.ink },
  removeBtn: { backgroundColor: colors.dangerSoft },
  removeText: { color: colors.danger },
});
