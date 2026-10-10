export const quotaViews = [
  'bars',
  'pace',
  'pie',
  'fuel',
  'resets',
  'thresholds',
];
// Preserve saved Zone selections and older graph links after the rename.
export function normaliseQuotaView(view: unknown): string {
  if (view === 'zone' || view === 'consumption-pace') return 'pace';
  return typeof view === 'string' && quotaViews.includes(view) ? view : 'bars';
}
const key = 'codexometer.web.preferences.v1';
interface Preferences {
  statusLine: string[] | null;
  tab: 'quota' | 'sessions' | 'usage';
  view: string;
  selected: string;
  defaultDetail: number;
  layouts: { id: string; level: number }[];
}
const defaults: Preferences = {
  statusLine: null,
  tab: 'quota',
  view: 'bars',
  selected: '',
  defaultDetail: 0,
  layouts: [],
};
function read(): Preferences {
  try {
    const value = JSON.parse(localStorage.getItem(key) || 'null');
    if (!value || typeof value !== 'object') return defaults;
    return {
      statusLine: Array.isArray(value.statusLine)
        ? ([
            ...new Set(
              value.statusLine.filter(
                (id: unknown): id is string =>
                  typeof id === 'string' && id.length < 64,
              ),
            ),
          ].slice(0, 32) as string[])
        : null,
      tab: ['quota', 'sessions', 'usage'].includes(value.tab)
        ? value.tab
        : 'quota',
      view: normaliseQuotaView(value.view),
      selected:
        typeof value.selected === 'string' ? value.selected.slice(0, 256) : '',
      defaultDetail: value.defaultDetail === 1 ? 1 : 0,
      layouts: Array.isArray(value.layouts)
        ? value.layouts
            .filter(
              (entry: unknown): entry is { id: string; level: number } => {
                if (!entry || typeof entry !== 'object') return false;
                const item = entry as { id: unknown; level: unknown };
                return (
                  typeof item.id === 'string' &&
                  item.id.length <= 256 &&
                  [0, 1, 2].includes(item.level as number)
                );
              },
            )
            .slice(-100)
        : [],
    };
  } catch {
    return defaults;
  }
}
export const preferences = $state<Preferences>(read());
export const statusLineUI = $state({ open: false });
export function savePreferences() {
  const value = JSON.stringify(preferences);
  try {
    localStorage.setItem(key, value);
  } catch {
    /* Optional, memory-only fallback. */
  }
}
export function homeRoute() {
  return preferences.tab === 'quota'
    ? '/quota/' + preferences.view
    : '/' + preferences.tab;
}
export function detailLevel(id: string) {
  return (
    preferences.layouts.find((entry) => entry.id === id)?.level ??
    preferences.defaultDetail
  );
}
export function setAllDetailLevels(level: 0 | 1) {
  preferences.defaultDetail = level;
  preferences.layouts = [];
}
export function setDetailLevel(id: string, level: number) {
  preferences.layouts = [
    ...preferences.layouts.filter((entry) => entry.id !== id),
    { id, level: Math.max(0, Math.min(2, level)) },
  ].slice(-100);
}
