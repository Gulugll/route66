import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native';

import type { Place } from '../api/types';
import { colors, radius, spacing } from '../theme/tokens';

interface Props {
  query: string;
  onQueryChange: (q: string) => void;
  city: string;
  onCityChange: (c: string) => void;
  results: Place[];
  loading: boolean;
  error: string | null;
  onPick: (place: Place) => void;
}

export function SearchBar({ query, onQueryChange, city, onCityChange, results, loading, error, onPick }: Props) {
  return (
    <View style={styles.wrap}>
      <TextInput
        style={styles.input}
        value={query}
        onChangeText={onQueryChange}
        placeholder="搜索地点，点结果加入列表"
        placeholderTextColor={colors.textTertiary}
        returnKeyType="search"
      />
      <TextInput
        style={[styles.input, styles.cityInput]}
        value={city}
        onChangeText={onCityChange}
        placeholder="城市(可选)"
        placeholderTextColor={colors.textTertiary}
        returnKeyType="done"
      />
      {error ? <Text style={styles.error}>{error}</Text> : null}
      {!error && loading ? <Text style={styles.hint}>搜索中…</Text> : null}
      {results.map((p) => (
        <Pressable
          key={`${p.lat},${p.lng},${p.name}`}
          style={({ pressed }) => [styles.candidate, pressed && styles.candidatePressed]}
          onPress={() => onPick(p)}
        >
          <Text style={styles.candidateName} numberOfLines={1}>
            {p.name}
          </Text>
          <Text style={styles.candidateAddr} numberOfLines={1}>
            {p.address}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: spacing.sm },
  input: {
    height: 38,
    borderRadius: radius.input,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.bgSubtle,
    paddingHorizontal: 10,
    fontSize: 14,
    color: colors.ink,
  },
  cityInput: { height: 32, fontSize: 12 },
  error: { fontSize: 12, color: colors.danger },
  hint: { fontSize: 12, color: colors.textTertiary },
  candidate: {
    borderRadius: radius.chip,
    backgroundColor: colors.bgSunken,
    paddingHorizontal: 10,
    paddingVertical: 6,
  },
  candidatePressed: { backgroundColor: colors.drivingSoft },
  candidateName: { fontSize: 14, color: colors.ink, fontWeight: '500' },
  candidateAddr: { fontSize: 11, color: colors.textSecondary },
});
