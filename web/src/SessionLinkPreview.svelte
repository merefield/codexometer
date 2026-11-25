<script lang="ts">
  import { onDestroy, tick, untrack } from 'svelte';
  import SavedSessions from './SavedSessions.svelte';
  import SessionTranscript from './SessionTranscript.svelte';
  import {
    live,
    previewSessionLink,
    sessionLinks,
    type ThreadLinkPreview,
  } from './state.svelte';

  let { scope, liveIDs }: { scope: string; liveIDs: Set<string> } = $props();
  let id = $derived(sessionLinks.ids.at(-1));
  let panel = $state<HTMLDialogElement>();
  let body = $state<HTMLDivElement>();
  let closeButton = $state<HTMLButtonElement>();
  let page = $state<ThreadLinkPreview>();
  let loading = $state(false);
  let savedResume = $state(false);
  let source: HTMLElement | undefined;
  let last = '';
  const offsets = new Map<string, number>();

  function back() {
    sessionLinks.ids = sessionLinks.ids.slice(0, -1);
  }

  function open() {
    if (!page) return;
    location.hash = '/sessions/' + encodeURIComponent(page.root.id);
    sessionLinks.ids = [];
  }

  $effect(() => {
    const key = id;
    const dialog = panel;
    const wantedScope = sessionLinks.scope;
    const currentScope = scope;
    if (!dialog) return;
    if (key && wantedScope !== currentScope) {
      sessionLinks.ids = [];
      return;
    }
    if (!key) {
      if (dialog.open) dialog.close();
      source?.focus({ preventScroll: true });
      source = undefined;
      offsets.clear();
      last = '';
      return;
    }
    if (!dialog.open) {
      if (document.activeElement instanceof HTMLElement)
        source = document.activeElement;
      dialog.showModal();
    }
    return untrack(() => {
      if (last && body) offsets.set(last, body.scrollTop);
      last = key;
      savedResume = false;
      page = undefined;
      loading = true;
      closeButton?.focus({ preventScroll: true });
      const controller = new AbortController();
      void (async () => {
        try {
          const next = await previewSessionLink?.(
            wantedScope,
            key,
            controller.signal,
          );
          if (!controller.signal.aborted) {
            savedResume =
              !!next &&
              !next.agent &&
              !liveIDs.has(next.root.id) &&
              !!live.data?.control;
            page = next;
          }
        } catch {
          // Missing, stale, and unauthorized targets use the same message.
        } finally {
          if (!controller.signal.aborted) loading = false;
        }
        await tick();
        if (!controller.signal.aborted && body)
          body.scrollTop = offsets.get(key) || 0;
      })();
      return () => controller.abort();
    });
  });
  onDestroy(() => {
    sessionLinks.ids = [];
  });
</script>

<dialog
  bind:this={panel}
  aria-labelledby="linked-session-heading"
  oncancel={(event) => {
    event.preventDefault();
    back();
  }}
  onkeydown={(event) => event.stopPropagation()}
>
  <div class="spread">
    <h2 id="linked-session-heading">
      {page?.agent ? 'Agent transcript' : 'Session preview'}{page
        ? ` // ${page.agent?.name || (page.agent ? page.agent.id.slice(-5) : page.root.name || page.root.id.slice(-5))}`
        : ''}
    </h2>
    <button type="button" bind:this={closeButton} onclick={back}>Back</button>
  </div>
  <div class="body" bind:this={body}>
    {#if loading}<p role="status">Loading…</p>
    {:else if !page}<p role="status">Session unavailable here.</p>
    {:else if savedResume}<SavedSessions
        {scope}
        {liveIDs}
        target={{ row: page.root, turns: page.turns }}
      />
    {:else}
      <SessionTranscript turns={page.turns} unverified={page.unverifiedTurns} />
    {/if}
  </div>
  {#if page && !page.agent && !savedResume && liveIDs.has(page.root.id)}<button
      type="button"
      onclick={open}>Open session</button
    >{/if}
</dialog>

<style>
  dialog {
    color: var(--ink);
    background: var(--bg);
    border: 1px solid var(--edge);
    width: min(78rem, calc(100vw - 2rem));
    max-height: calc(100dvh - 2rem);
    overflow: hidden;
    padding: 0.75rem;
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 68%);
  }
  .body {
    max-height: calc(100dvh - 10rem);
    overflow: auto;
  }
  h2 {
    overflow-wrap: anywhere;
  }
</style>
