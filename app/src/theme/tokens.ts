/**
 * tokens.ts —— 设计变量单一来源（RN 侧）
 *
 * 值与 frontend/src/styles/tokens.css 一一对应；旧前端删除后本文件是唯一来源。
 * 地图折线颜色（Leaflet 侧）与界面颜色同源于此，避免出现第二个同步点。
 */

export const colors = {
  bgPage: '#F5F5F7',
  bgSurface: '#FFFFFF',
  bgSubtle: '#FAFAFC',
  bgSunken: '#F0F0F2',
  border: '#E0E0E0',
  borderSoft: '#EFEFF1',

  ink: '#1D1D1F',
  textSecondary: '#7A7A7A',
  textTertiary: '#A1A1A6',

  // 语义色：只表示出行方式，不参与界面装饰（蓝 #0066CC 专属"驾车"）
  driving: '#0066CC',
  walking: '#34A853',
  transit: '#E08600',
  drivingSoft: '#E8F1FB',

  danger: '#D93025',
  dangerSoft: '#FDECEA',
  success: '#34A853',
  warningSoft: '#FFF8E6',
  warningBorder: '#F2D18A',
  warningText: '#8A5A00',

  accent: '#6E56CF',
  accentSoft: '#EFEEFB',
} as const;

export const radius = {
  input: 9,
  card: 12,
  chip: 10,
} as const;

export const spacing = {
  xs: 6,
  sm: 8,
  md: 12,
  lg: 20,
} as const;

export const modeColors: Record<string, string> = {
  driving: colors.driving,
  walking: colors.walking,
  transit: colors.transit,
};

/** 出行方式展示标签（ModeSelector / PointList 共用，与 modeColors 同源） */
export const modeLabels: Array<{ value: string; label: string }> = [
  { value: 'driving', label: '🚗' },
  { value: 'walking', label: '🚶' },
  { value: 'transit', label: '🚌' },
];
