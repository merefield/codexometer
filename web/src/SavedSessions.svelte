<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { requiresConfirmation } from './confirmation';
  import SessionSearch from './SessionSearch.svelte';
  import SessionTranscript from './SessionTranscript.svelte';
  import {
    browseSaved,
    ControlRejected,
    live,
    resumePicker,
    sessionLinks,
    resumeSaved,
    type SavedRow,
    type SavedPage,
  } from './state.svelte';
  let {
    scope,
    liveIDs,
    target,
    resumeBusy = $bindable(false),
  }: {
    scope: string;
    liveIDs: Set<string>;
    target?: { row: SavedRow; turns?: SavedPage['turns'] };
    resumeBusy?: boolean;
  } = $props();
  let query = $state('');
  let search = $state<HTMLInputElement>();
  let rows = $state<(SavedRow & { cursor: string })[]>([]);
  let next = $state('');
  let id = $state('');
  let turns = $state<SavedPage['turns']>();
  let busy = $state(false);
  let failed = $state(false);
  let resuming = $state('');
  let resumeNotice = $state('');
  let review = $state<{ id: string; token: string } | null>(null);
  let now = $state(Date.now());
  let resumeTimer: ReturnType<typeof setTimeout>;
  let controller: AbortController | undefined;
  let disposed = false;
  let resumeLocked = $derived(resumeBusy || !!resuming);
  let matches = $derived(
    rows.filter(
      (row) => !!target || !liveIDs.has(row.id) || row.id === resuming,
    ),
  );
  async function load(cursor: string, detail = '', term = query.trim()) {
    controller?.abort();
    const request = new AbortController();
    controller = request;
    id = detail;
    turns = undefined;
    busy = true;
    failed = false;
    try {
      if (!browseSaved) throw new Error();
      const page = await browseSaved(
        scope,
        cursor,
        detail,
        request.signal,
        term,
      );
      if (request.signal.aborted) return;
      if (detail) turns = page.turns || [];
      else {
        const seen = new Set(rows.map((r) => r.id));
        rows = [
          ...rows,
          ...(page.sessions || [])
            .filter((r) => !seen.has(r.id) && seen.add(r.id))
            .map((r) => ({ ...r, cursor: r.listCursor || cursor })),
        ];
        next = page.next !== cursor ? page.next || '' : '';
      }
    } catch {
      if (!request.signal.aborted) {
        failed = true;
        if (!cursor || detail) {
          rows = [];
          next = '';
        }
        turns = undefined;
      }
    } finally {
      if (!request.signal.aborted) busy = false;
    }
  }
  onMount(() => {
    search?.focus();
    const clock = setInterval(() => (now = Date.now()), 15000);
    return () => {
      disposed = true;
      controller?.abort();
      clearTimeout(resumeTimer);
      clearInterval(clock);
    };
  });
  function age(updatedAt: number | undefined, now: number) {
    if (!updatedAt || updatedAt < 0) return '—';
    const minutes = Math.floor(Math.max(now / 1000 - updatedAt, 0) / 60);
    if (minutes >= 1440) return `${Math.floor(minutes / 1440)}d ago`;
    if (minutes >= 60) return `${Math.floor(minutes / 60)}h ago`;
    if (minutes) return `${minutes}m ago`;
    return 'Just now';
  }
  $effect(() => {
    if (target) {
      rows = [{ ...target.row, cursor: '' }];
      id = target.row.id;
      turns = target.turns || [];
      return;
    }
    const term = query.trim();
    // Coalesce simultaneous input events within a frame, not with a typing
    // delay. The loopback request reads Codexometer's in-memory name index.
    const frame = requestAnimationFrame(() => void load('', '', term));
    untrack(() => {
      controller?.abort();
      busy = true;
      failed = false;
      rows = [];
      id = '';
      review = null;
      turns = undefined;
      next = '';
    });
    return () => cancelAnimationFrame(frame);
  });
  $effect(() => {
    if (resuming && liveIDs.has(resuming)) {
      location.hash = '/sessions/' + encodeURIComponent(resuming);
      if (target) sessionLinks.ids = [];
      else resumePicker.open = false;
    }
  });
  async function resume(row: SavedRow & { cursor: string }) {
    if (!resumeSaved || resumeLocked) return;
    const preparing =
      requiresConfirmation(live.data?.confirmationMode, true) &&
      review?.id !== row.id;
    resumeNotice = '';
    resumeBusy = true;
    try {
      if (preparing) {
        const token = await resumeSaved(scope, row.cursor, row.id, {
          mode: 'prepare',
        });
        if (!disposed && id === row.id) review = { id: row.id, token };
        return;
      }
      const confirmation = review?.id === row.id ? review.token : '';
      review = null;
      await resumeSaved(
        scope,
        row.cursor,
        row.id,
        confirmation ? { confirmation } : undefined,
      );
      if (disposed) return;
      resuming = row.id;
      resumeTimer = setTimeout(() => {
        resumeNotice =
          'Resume accepted; still waiting for the session to appear.';
      }, 15000);
    } catch (error) {
      if (disposed) return;
      if (!preparing && !(error instanceof ControlRejected)) {
        resuming = row.id;
        resumeNotice =
          'Resume outcome unknown. Check Codex before trying again.';
      } else {
        resumeNotice =
          error instanceof ControlRejected
            ? error.message
            : 'Could not prepare the resume.';
      }
    } finally {
      if (!disposed) resumeBusy = false;
    }
  }
</script>

<section class="panel" aria-label="Saved sessions">
  {#if !target}
    <h2>SAVED // SESSIONS</h2>
    <SessionSearch
      label="Search saved sessions"
      placeholder="Search by session name"
      disabled={resumeLocked}
      bind:field={search}
      bind:value={query}
    />
    <button disabled={resumeBusy} onclick={() => (resumePicker.open = false)}
      >Close</button
    >
  {/if}
  {#if resuming || resumeNotice}
    <p role="status">{resumeNotice || 'Opening the resumed session…'}</p>
  {/if}
  {#if matches.length && !target}
    <div class="saved-columns muted">
      <span>Session</span><span>Time ago</span>
    </div>
  {/if}
  {#each matches as row (row.id)}
    {#if !target}
      <button
        class="saved-columns saved-row"
        disabled={resumeLocked}
        aria-expanded={id === row.id}
        onclick={() => {
          review = null;
          if (id === row.id) {
            controller?.abort();
            id = '';
            busy = false;
            failed = false;
          } else void load(row.cursor, row.id);
        }}
      >
        <span class="saved-session-name"
          >{row.id.slice(-5).toUpperCase()} // {row.name || row.directory}</span
        >
        <span class="muted saved-age">{age(row.updatedAt, now)}</span>
      </button>
    {/if}
    {#if id === row.id && turns}
      <div inert={!!target && resumeBusy}>
        <SessionTranscript {turns} />
      </div>
      {#if live.data?.control}
        <p class="muted">
          If this session has an active goal, Codex may continue it
          automatically.
        </p>
        {#if review?.id === row.id}
          <div
            class="profile-review"
            role="group"
            aria-label="Review resume session"
          >
            <p>Resume {row.name || row.directory || row.id}?</p>
            <button disabled={resumeBusy} onclick={() => void resume(row)}
              >Confirm resume</button
            >
            <button onclick={() => (review = null)}>Cancel</button>
          </div>
        {:else}
          <button disabled={resumeLocked} onclick={() => void resume(row)}
            >Resume</button
          >
        {/if}
      {/if}
    {/if}
  {/each}
  {#if busy}<p role="status">Loading saved sessions…</p>{:else if failed}<p
      role="status"
    >
      {next
        ? 'Could not load older sessions. Try again.'
        : 'Saved history unavailable. Live filtering still works.'}
    </p>{:else if !matches.length && !next}<p>
      {query.trim()
        ? 'No saved session names match this search.'
        : 'No saved sessions to show.'}
    </p>{/if}
  {#if next}<button disabled={busy || resumeLocked} onclick={() => load(next)}
      >{query.trim()
        ? 'Search older sessions'
        : 'Load more saved sessions'}</button
    >{/if}
</section>

<style>
  .saved-columns {
    display: grid;
    grid-template-columns: minmax(0, 1fr) max-content;
    column-gap: 0.75em;
    align-items: baseline;
    text-align: left;
  }
  .saved-columns:not(button) {
    padding-inline: 0.5em;
  }
  .saved-row {
    width: 100%;
  }
  .saved-session-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .saved-age {
    white-space: nowrap;
  }
</style>
