<script lang="ts">
  import { onMount } from 'svelte';
  import { browseSaved, type SavedRow, type SavedPage } from './state.svelte';
  import { matchesSession } from './session-filter';
  let {
    scope,
    query,
    liveIDs,
  }: { scope: string; query: string; liveIDs: Set<string> } = $props();
  let rows = $state<(SavedRow & { cursor: string })[]>([]);
  let next = $state('');
  let id = $state('');
  let turns = $state<SavedPage['turns']>();
  let busy = $state(false);
  let failed = $state(false);
  let controller: AbortController | undefined;
  let matches = $derived(
    rows.filter((row) => !liveIDs.has(row.id) && matchesSession(query, row)),
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
      const page = await browseSaved(scope, cursor, detail, request.signal);
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
    void load('');
    return () => controller?.abort();
  });
</script>

<section class="panel" aria-label="Saved sessions">
  <h2>SAVED SESSIONS // READ ONLY</h2>
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
      {#each [...turns].reverse() as turn}
        {#if turn.task}<p class="muted">{turn.task}</p>{/if}
        <p class="saved-reply">
          {turn.restricted ? 'View this reply in Codex.' : turn.reply}
        </p>
      {:else}<p>No completed turns available.</p>{/each}
    {/if}
  {/each}
  {#if busy}<p role="status">Loading saved sessions…</p>{:else if failed}<p
      role="status"
    >
      Saved history unavailable. Live filtering still works.
    </p>{:else if !matches.length}<p>
      No matches in the saved sessions loaded so far.
    </p>{/if}
  {#if next}<button disabled={busy} onclick={() => load(next)}
      >Load more saved sessions</button
    >{/if}
</section>

<style>
  .saved-reply {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
