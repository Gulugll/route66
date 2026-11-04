import { StyleSheet, Text, View } from 'react-native';

import { colors, radius, spacing } from '../theme/tokens';

interface Props {
  warnings: string[];
}

/** 降级警告条：哪些数字是估算的、哪些段没画出路网，必须让用户看见（is_degraded 原则） */
export function WarningBar({ warnings }: Props) {
  if (warnings.length === 0) return null;
  return (
    <View style={styles.box}>
      {warnings.map((w, i) => (
        <Text key={i} style={styles.text}>
          ⚠️ {w}
        </Text>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  box: {
    backgroundColor: colors.warningSoft,
    borderColor: colors.warningBorder,
    borderWidth: 1,
    borderRadius: radius.chip,
    padding: spacing.md,
    gap: spacing.xs,
  },
  text: { fontSize: 12, color: colors.warningText, lineHeight: 17 },
});
