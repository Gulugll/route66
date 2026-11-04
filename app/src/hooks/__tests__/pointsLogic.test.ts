import { addPoint, removePoint, movePoint, nextPointId, type RnPoint } from '../pointsLogic';

const A: RnPoint = { id: 'p0', name: '天安门', lat: 39.9087, lng: 116.3975 };
const B: RnPoint = { id: 'p1', name: '故宫', lat: 39.9163, lng: 116.3972 };
const C: RnPoint = { id: 'p2', name: '天坛', lat: 39.8822, lng: 116.4066 };

describe('pointsLogic', () => {
  it('addPoint 追加到末尾', () => {
    expect(addPoint([A], B)).toEqual([A, B]);
  });

  it('addPoint 不改动原数组（不可变更新）', () => {
    const src = [A];
    const out = addPoint(src, B);
    expect(src).toEqual([A]);
    expect(out).not.toBe(src);
  });

  it('removePoint 按 id 删除', () => {
    expect(removePoint([A, B, C], 'p1')).toEqual([A, C]);
    expect(removePoint([A, B, C], 'nope')).toEqual([A, B, C]);
  });

  it('movePoint 上移/下移并夹住边界', () => {
    expect(movePoint([A, B, C], 'p2', -1)).toEqual([A, C, B]);
    expect(movePoint([A, B, C], 'p0', -1)).toEqual([A, B, C]); // 顶部上移=不变
    expect(movePoint([A, B, C], 'p2', 1)).toEqual([A, B, C]); // 底部下移=不变
    expect(movePoint([A, B, C], 'p0', 1)).toEqual([B, A, C]);
  });

  it('nextPointId 取最大 id +1，永不与现有 id 冲突', () => {
    expect(nextPointId([A, C])).toBe('p3'); // p0、p2 → max+1，不填空隙
    expect(nextPointId([A, B, C])).toBe('p3');
    expect(nextPointId([])).toBe('p0');
  });
});
