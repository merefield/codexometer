export interface Sample {
  at: string;
  tokens: number;
}
export interface Session {
  id: string;
  directory: string;
  tokens: number;
  agents: number;
  status: string;
  contextKind: string;
  text: string;
  command: string;
  source: string;
  activity: string;
  samples: Sample[] | null;
}
export interface Meter {
  name: string;
  used: number;
  duration: number | null;
  reset: number | null;
  details: string;
}
export interface Credit {
  title: string;
  status: string;
  expires: number | null;
  expiryKnown: boolean;
}
export interface Usage {
  summary: {
    lifetimeTokens: number | null;
    peakDailyTokens: number | null;
    currentStreakDays: number | null;
  };
  dailyUsageBuckets: { startDate: string; tokens: number }[] | null;
}
export interface Snapshot {
  version: string;
  meters: Meter[];
  credits: Credit[];
  creditCount: number;
  sessions: Session[];
  usage: Usage | null;
  quotaAt: string;
  sessionsAt: string;
  usageAt: string;
  quotaError: boolean;
  sessionsError: boolean;
  usageError: boolean;
}

export const live = $state<{
  data: Snapshot | null;
  connected: boolean;
  error: string;
}>({ data: null, connected: false, error: '' });
export const number = (n: number | null | undefined) =>
  n == null ? 'Unavailable' : n.toLocaleString('en-GB');
export const date = (s: string | number | null | undefined) =>
  !s || String(s).startsWith('0001-')
    ? 'Not yet available'
    : new Date(typeof s === 'number' ? s * 1000 : s).toLocaleString('en-GB');

const storageKey = 'codexometer.web.session';

// Only this module owns the temporary browser capability. It is never a Codex
// credential. sessionStorage is origin- and tab-scoped and permits page reload;
// it is not an XSS boundary. Never place this token in links, logs or localStorage.
export function connect(): () => void {
  const controller = new AbortController();
  let retry: ReturnType<typeof setTimeout>;
  let token = '';
  try {
    token = sessionStorage.getItem(storageKey) || '';
  } catch {
    /* Memory-only if storage disabled. */
  }
  const fragment = location.hash;
  const secret = fragment.startsWith('#pair=') ? fragment.slice(6) : '';
  if (secret) {
    history.replaceState(null, '', '/#/quota/bars');
    window.dispatchEvent(new HashChangeEvent('hashchange'));
  }

  async function start() {
    if (secret) {
      try {
        const response = await fetch('/api/pair', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ secret }),
          signal: controller.signal,
        });
        if (!response.ok)
          throw new Error(
            'Pairing link expired or already used. Restart --web for a fresh link.',
          );
        token = (await response.json()).token;
        try {
          sessionStorage.setItem(storageKey, token);
        } catch {
          /* Still works until reload. */
        }
      } catch (error) {
        if (!controller.signal.aborted)
          live.error =
            error instanceof Error ? error.message : 'Pairing failed';
        return;
      }
    }
    if (!token) {
      live.error =
        'Open the private pairing link printed by codexometer --web in your terminal.';
      return;
    }
    await stream();
  }

  async function stream() {
    try {
      const response = await fetch('/api/events', {
        headers: { Authorization: `Bearer ${token}` },
        signal: controller.signal,
        cache: 'no-store',
      });
      if (response.status === 401) {
        try {
          sessionStorage.removeItem(storageKey);
        } catch {
          /* Nothing to clear. */
        }
        live.error =
          'Browser session expired. Restart --web and open its new pairing link.';
        return;
      }
      if (!response.ok || !response.body)
        throw new Error('Live connection unavailable');
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let pending = '';
      while (!controller.signal.aborted) {
        const { done, value } = await reader.read();
        if (done) break;
        pending += decoder.decode(value, { stream: true });
        let end: number;
        while ((end = pending.indexOf('\n\n')) >= 0) {
          const event = pending.slice(0, end);
          pending = pending.slice(end + 2);
          if (event.startsWith('data: ')) {
            live.data = JSON.parse(event.slice(6));
            live.connected = true;
            live.error = '';
          }
        }
      }
    } catch {
      /* Reconnect below with a full snapshot, not partial event replay. */
    } finally {
      live.connected = false;
    }
    if (!controller.signal.aborted) {
      live.error = 'Connection lost — showing last observation. Reconnecting…';
      retry = setTimeout(stream, 2500);
    }
  }

  void start();
  return () => {
    controller.abort();
    clearTimeout(retry);
    live.connected = false;
  };
}
