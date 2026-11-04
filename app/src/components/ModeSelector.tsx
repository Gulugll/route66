import { Pressable, StyleSheet, Text, View } from 'react-native';

import type { Mode } from '../api/types';
import { colors } from '../theme/tokens';

const MODES: Array<{ value: Mode; label: string }> = [
  { value: 'driving', label: '🚗 驾车' },
  { value: 'walking', label: '🚶 步行' },
  { value: 'transit', label: '🚌 公交' },
];

interface Props {
  mode: Mode;
  onModeChange: (m: Mode) => void;
  manual: boolean;
  onManualChange: (v: boolean) => void;
}

/** 整体出行方式分段按钮组 + "按列表顺序"开关（manual=true 跳过 TSP） */
export function ModeSelector({ mode, onModeChange, manual, onManualChange }: Props) {
  return (
    <View style={styles.wrap}>
      <View style={styles.group}>
        {MODES.map((m) => (
          <Pressable
            key={m.value}
            style={[styles.btn, mode === m.value && styles.btnActive]}
            onPress={() => onModeChange(m.value)}
          >
            <Text style={[styles.text, mode === m.value && styles.textActive]}>{m.label}</Text>
          </Pressable>
        ))}
      </View>
      <Pressable style={styles.manualRow} onPress={() => onManualChange(!manual)}>
        <View style={[styles.checkbox, manual && styles.checkboxOn]}>
          {manual ? <Text style={styles.checkMark}>✓</Text> : null}
        </View>
        <Text style={styles.manualText}>按列表顺序（不自动优化）</Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: 8 },
  group: { flexDirection: 'row', gap: 6 },
  btn: {
    flex: 1,
    height: 34,
    borderRadius: 8,
    backgroundColor: colors.bgSunken,
    alignItems: 'center',
    justifyContent: 'center',
  },
  btnActive: { backgroundColor: colors.ink },
  text: { fontSize: 13, color: colors.ink },
  textActive: { color: '#FFFFFF', fontWeight: '600' },
  manualRow: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  checkbox: {
    width: 16,
    height: 16,
    borderRadius: 4,
    borderWidth: 1.5,
    borderColor: colors.border,
    alignItems: 'center',
    justifyContent: 'center',
  },
  checkboxOn: { backgroundColor: colors.ink, borderColor: colors.ink },
  checkMark: { fontSize: 11, color: '#FFFFFF', lineHeight: 13 },
  manualText: { fontSize: 12, color: colors.textSecondary },
});
