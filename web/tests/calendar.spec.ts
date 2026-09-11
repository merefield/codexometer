import { test, expect } from '@playwright/test';
import { usageRange } from '../src/calendar';

test('usage ranges clamp month ends and leap years', () => {
  for (const [now, months, start] of [
    ['2026-03-31', 6, '2025-10-01'],
    ['2026-08-31', 6, '2026-03-01'],
    ['2024-08-31', 6, '2024-03-01'],
    ['2024-02-29', 12, '2023-03-01'],
    ['2026-01-31', 12, '2025-02-01'],
  ] as const) {
    const range = usageRange(new Date(now + 'T23:59:59Z'), months, 0);
    expect(range.start.toISOString()).toBe(start + 'T00:00:00.000Z');
    expect(range.end.toISOString()).toBe(now + 'T00:00:00.000Z');
  }
});

test('earlier usage ranges have no gaps or overlaps', () => {
  for (const now of ['2026-03-31', '2024-02-29', '2026-08-31']) {
    for (const months of [6, 12]) {
      const date = new Date(now);
      let newer = usageRange(date, months, 0);
      for (let offset = 1; offset <= 12; offset++) {
        const older = usageRange(date, months, offset);
        expect(newer.start.getTime() - older.end.getTime()).toBe(86_400_000);
        expect(older.start.getTime()).toBeLessThan(older.end.getTime());
        newer = older;
      }
    }
  }
});
