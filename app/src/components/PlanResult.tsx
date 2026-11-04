import { StyleSheet, Text, View } from 'react-native';

import type { PlanResult } from '../api/types';
import { colors, modeLabels, radius, spacing } from '../theme/tokens';

interface Props {
  result: PlanResult;
  points: Array<{ name: string }>;
  orderIdx: number[];
  /** 每段方式（长度 = 点数-1），混合出行时逐行显示到下一段怎么走 */
  segmentModes?: string[];
}

/**
 * 规划结果：按 order（人看名字）列访问顺序，总里程 tabular 数字。
 * 顺序展示用后端 order_idx（机器下标）映射回本地点位，名字只给人看。
 */
export function PlanResult({ result, points, orderIdx, segmentModes }: Props) {
  // order_idx 含起点（0 = origin），直接映射完整访问顺序
  const orderedNames = orderIdx.map((i) => points[i]?.name ?? '?');
  const legEmoji = (i: number): string => {
    const m = segmentModes?.[i];
    return modeLabels.find((x) => x.value === m)?.label ?? '';
  };
  return (
    <View style={styles.card}>
      <Text style={styles.heading}>最优顺序</Text>
      <View style={styles.route}>
        {orderedNames.map((name, i) => (
          <Text key={i} style={styles.stop}>
            {i + 1}. {name}
            {i < orderedNames.length - 1 ? `  ${legEmoji(i)}→` : ''}
          </Text>
        ))}
      </View>
      <Text style={styles.total}>总计 {result.total_km.toFixed(1)} km</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    borderRadius: radius.card,
    borderWidth: 1,
    borderColor: colors.borderSoft,
    backgroundColor: colors.bgSurface,
    padding: spacing.md,
    gap: spacing.sm,
  },
  heading: { fontSize: 13, fontWeight: '600', color: colors.ink },
  route: { gap: 2 },
  stop: { fontSize: 14, color: colors.ink, lineHeight: 20 },
  total: {
    fontSize: 15,
    fontWeight: '600',
    color: colors.ink,
    fontVariant: ['tabular-nums'],
  },
});
