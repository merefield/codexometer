const dayMS = 86_400_000;

function shiftMonthsClamped(date: Date, months: number): Date {
  const shifted = new Date(date);
  shifted.setUTCDate(1);
  shifted.setUTCMonth(shifted.getUTCMonth() + months);
  const lastDay = new Date(
    Date.UTC(shifted.getUTCFullYear(), shifted.getUTCMonth() + 1, 0),
  ).getUTCDate();
  shifted.setUTCDate(Math.min(date.getUTCDate(), lastDay));
  return shifted;
}

// Inclusive UTC ranges: each older page ends the day before the newer page
// starts. Clamp month-end dates rather than letting Date normalize into the
// following month (e.g. March 31 minus six months must land on September 30).
export function usageRange(
  now: Date,
  months: number,
  offset: number,
): { start: Date; end: Date } {
  let end = new Date(
    Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()),
  );
  for (let page = 0; ; page++) {
    const previousEnd = shiftMonthsClamped(end, -months);
    const start = new Date(previousEnd.getTime() + dayMS);
    if (page === offset) return { start, end };
    end = previousEnd;
  }
}
