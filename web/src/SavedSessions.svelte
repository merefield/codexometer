<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import SessionSearch from './SessionSearch.svelte';
  import SessionTranscript from './SessionTranscript.svelte';
  import {
    browseSaved,
    live,
    resumePicker,
    resumeSaved,
    type SavedRow,
    type SavedPage,
  } from './state.svelte';
  let { scope, liveIDs }: { scope: string; liveIDs: Set<string> } = $props();
  let query = $state('');
  let search = $state<HTMLInputElement>();
  let rows = $state<(SavedRow & { cursor: string })[]>([]);
  let next = $state('');
  let id = $state('');
  let turns = $state<SavedPage['turns']>();
  let busy = $state(false);
  let failed = $state(false);
  let resumeBusy = $state(false);
  let resuming = $state('');
  let resumeNotice = $state('');
  let resumeTimer: ReturnType<typeof setTimeout>;
  let controller: AbortController | undefined;
  let matches = $derived(
    rows.filter((row) => !liveIDs.has(row.id) || row.id === resuming),
  );
  async function load(cursor: string, detail = '') {
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
        query.trim(),
      );
      if (request.signal.aborted) return;
      if (detail) turns = page.turns || [];
      else {
        const seen = new Set(rows.map((r) => r.id));
        rows = [
          ...rows,
          ...(page.sessions || [])
            .filter((r) => !seen.has(r.id) && seen.add(r.id))
            .map((r) => ({ ...r, cursor })),
        ];
        next = page.next !== cursor ? page.next || '' : '';
      }
    } catch {
      if (!request.signal.aborted) {
        failed = true;
        rows = [];
        next = '';
        turns = undefined;
      }
    } finally {
      if (!request.signal.aborted) busy = false;
    }
  }
  onMount(() => {
    search?.focus();
    return () => {
      controller?.abort();
      clearTimeout(resumeTimer);
    };
  });
  $effect(() => {
    const term = query.trim();
    const timer = setTimeout(() => void load(''), term ? 180 : 0);
    untrack(() => {
      controller?.abort();
      rows = [];
      id = '';
      turns = undefined;
      next = '';
    });
    return () => clearTimeout(timer);
  });
  $effect(() => {
    if (resuming && liveIDs.has(resuming)) {
      location.hash = '/sessions/' + encodeURIComponent(resuming);
      resumePicker.open = false;
    }
  });
  async function resume(row: SavedRow & { cursor: string }) {
    if (!resumeSaved || resumeBusy || resuming) return;
    resumeNotice = '';
    resumeBusy = true;
    try {
      await resumeSaved(scope, row.cursor, row.id);
      resuming = row.id;
      resumeTimer = setTimeout(() => {
        resumeNotice = 'Session did not appear in the live list. Check Codex.';
        resuming = '';
      }, 15000);
    } catch {
      resumeNotice = 'Resume not confirmed. Check Codex.';
    } finally {
      resumeBusy = false;
    }
  }
</script>

<section class="panel" aria-label="Saved sessions">
  <h2>SAVED SESSIONS</h2>
  <SessionSearch
    label="Search saved sessions"
    placeholder="Search saved conversations"
    bind:field={search}
    bind:value={query}
  />
  <button onclick={() => (resumePicker.open = false)}>Close</button>
  {#if resuming}<p role="status">
      Opening the resumed session…
    </p>{:else if resumeNotice}<p role="status">{resumeNotice}</p>{/if}
  {#each matches as row (row.id)}
    <button
      aria-expanded={id === row.id}
      onclick={() => {
        if (id === row.id) {
          controller?.abort();
          id = '';
          busy = false;
          failed = false;
        } else void load(row.cursor, row.id);
      }}
      >SAVED {row.id.slice(-5).toUpperCase()} // {row.name ||
        row.directory}</button
    >
    {#if id === row.id && turns}
      <SessionTranscript {turns} />
      {#if live.data?.control}<button
          disabled={resumeBusy || !!resuming}
          onclick={() => void resume(row)}>Resume</button
        >{/if}
    {/if}
  {/each}
  {#if busy}<p role="status">Loading saved sessions…</p>{:else if failed}<p
      role="status"
    >
      Saved history unavailable. Live filtering still works.
    </p>{:else if !matches.length}<p>
      No saved sessions match this search.
    </p>{/if}
  {#if next}<button disabled={busy} onclick={() => load(next)}
      >Load more saved sessions</button
    >{/if}
</section>
