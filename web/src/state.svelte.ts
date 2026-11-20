import { homeRoute } from './preferences.svelte';
import type { Snapshot } from './contracts.generated';
export type * from './contracts.generated';

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

export class ControlRejected extends Error {}

let sendControl:
  | ((action: string, body: unknown, signal?: AbortSignal) => Promise<unknown>)
  | undefined;
export type SavedRow = { id: string; name: string; directory: string };
export type SavedPage = {
  sessions?: SavedRow[];
  next?: string;
  turns?: { task: string; reply: string; restricted?: boolean }[];
};
export const resumePicker = $state({ open: false });
export type InspectionKind = 'agents' | 'terminals';
export const sessionInspector = $state<{ root: string; kind: InspectionKind }>({
  root: '',
  kind: 'agents',
});
export type SessionInspection = {
  agents?: { id: string; name: string; status: string }[];
  turns?: SavedPage['turns'];
  terminals?: {
    id: string;
    command: string;
    directory: string;
    pid?: number;
  }[];
  more?: boolean;
};
export let inspectSession:
  | ((
      root: string,
      child: string,
      kind: InspectionKind,
      signal: AbortSignal,
    ) => Promise<SessionInspection>)
  | undefined;
type SessionReview = { mode?: 'prepare'; confirmation?: string };
export let startSession:
  | ((
      scope: string,
      directory: string,
      prompt: string,
      review?: SessionReview,
    ) => Promise<{ id?: string; confirmation?: string }>)
  | undefined;
export let resumeSaved:
  | ((
      scope: string,
      cursor: string,
      id: string,
      review?: SessionReview,
    ) => Promise<string>)
  | undefined;
export let browseSaved:
  | ((
      scope: string,
      cursor: string,
      id: string,
      signal: AbortSignal,
      query?: string,
    ) => Promise<SavedPage>)
  | undefined;
export async function controlRequest<T>(
  action: 'offer' | 'prepare' | 'commit' | 'schedules' | 'commands' | 'read',
  body: unknown,
  signal?: AbortSignal,
): Promise<T> {
  if (
    !sendControl ||
    !live.connected ||
    !live.data?.control ||
    live.data.sessionsError
  )
    throw new Error('Session controls unavailable. Check the live connection.');
  return (await sendControl(action, body, signal)) as T;
}

// Only this module owns the temporary browser capability. It is never a Codex
// credential. sessionStorage is origin- and tab-scoped and permits page reload;
// it is not an XSS boundary. Never place this token in links, logs or localStorage.
export function connect(): () => void {
  const controller = new AbortController();
  let retry: ReturnType<typeof setTimeout>;
  let token = '';
  async function sessionRead(
    path: string,
    params: Record<string, string>,
    signal: AbortSignal,
    timeout: number,
  ) {
    const response = await fetch(path + '?' + new URLSearchParams(params), {
      headers: { Authorization: `Bearer ${token}` },
      signal: AbortSignal.any([
        controller.signal,
        signal,
        AbortSignal.timeout(timeout),
      ]),
      cache: 'no-store',
    });
    if (!response.ok) throw new Error('Session information unavailable');
    return response.json();
  }
  function postJSON(path: string, body: unknown, signal?: AbortSignal) {
    return fetch(path, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(body),
      signal: AbortSignal.any([
        controller.signal,
        AbortSignal.timeout(8000),
        ...(signal ? [signal] : []),
      ]),
      cache: 'no-store',
    });
  }
  inspectSession = async (root, agent, kind, signal) => {
    if (!live.connected || live.data?.sessionsError)
      throw new Error('Session unavailable');
    return sessionRead(
      '/api/sessions/inspect',
      { root, agent, kind },
      signal,
      6500,
    );
  };
  browseSaved = async (scope, cursor, id, signal, query = '') => {
    if (!live.connected || live.data?.savedScope !== scope)
      throw new Error('History unavailable');
    return sessionRead(
      '/api/sessions/saved',
      { scope, cursor, id, q: query },
      signal,
      8000,
    );
  };
  resumeSaved = async (scope, cursor, id, review) => {
    if (
      !live.connected ||
      !live.data?.control ||
      live.data.savedScope !== scope
    )
      throw new Error('Resume unavailable');
    const response = await postJSON('/api/sessions/resume', {
      scope,
      cursor,
      id,
      ...review,
    });
    if (!response.ok) throw new Error('Resume not confirmed. Check Codex.');
    if (review?.mode === 'prepare') return (await response.json()).confirmation;
    return '';
  };
  startSession = async (scope, directory, prompt, review) => {
    if (
      !live.connected ||
      !live.data?.control ||
      live.data.savedScope !== scope
    )
      throw new ControlRejected(
        'New sessions unavailable. Check the Codex connection.',
      );
    const response = await postJSON('/api/sessions/new', {
      scope,
      directory,
      prompt,
      ...review,
    });
    if (!response.ok) {
      if (response.status >= 500)
        throw new Error(
          'Could not verify the new session. Check Codex before trying again.',
        );
      throw new ControlRejected((await response.text()).trim());
    }
    return response.json();
  };
  sendControl = async (action, body, signal) => {
    const response = await postJSON('/api/control/' + action, body, signal);
    if (!response.ok) {
      if (response.status !== 502)
        throw new ControlRejected(
          'Action rejected, expired or changed. Refresh and check Codex; nothing was retried.',
        );
      throw new Error(
        'Outcome uncertain. Check Codex before taking another action; nothing was retried.',
      );
    }
    return response.json();
  };
  try {
    token = sessionStorage.getItem(storageKey) || '';
  } catch {
    /* Memory-only if storage disabled. */
  }
  const fragment = location.hash;
  const secret = fragment.startsWith('#pair=') ? fragment.slice(6) : '';
  if (secret || !fragment || fragment === '#') {
    history.replaceState(null, '', '/#' + homeRoute());
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
    sendControl = undefined;
    browseSaved = undefined;
    resumeSaved = undefined;
    resumePicker.open = false;
    inspectSession = undefined;
    sessionInspector.root = '';
    controller.abort();
    clearTimeout(retry);
    live.connected = false;
  };
}
