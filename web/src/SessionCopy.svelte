<script lang="ts">
  import type { Session } from './state.svelte';
  let { session, active = false }: { session: Session; active?: boolean } =
    $props();
  let available = $derived(
    ['LAST REPLY', 'LAST ACTIVITY'].includes(session.contextKind) &&
      !!session.text.trim(),
  );
  let busy = $state(false);
  let notice = $state('');
  let flashed = $state(false);
  $effect(() => {
    if (!flashed) return;
    const timer = setTimeout(() => {
      flashed = false;
    }, 150);
    return () => clearTimeout(timer);
  });
  $effect(() => {
    // Feedback belongs to this observed reply, not a later response.
    session.text;
    session.status;
    notice = '';
  });
  async function copy() {
    if (!available || busy) return;
    const text = session.text;
    busy = true;
    flashed = true;
    notice = '';
    try {
      await navigator.clipboard.writeText(text);
      if (session.text === text) notice = 'Copied.';
    } catch {
      if (session.text === text)
        notice =
          'Clipboard unavailable. Select the visible text and copy it manually.';
    } finally {
      busy = false;
    }
  }
  function keydown(event: KeyboardEvent) {
    if (
      !active ||
      !available ||
      event.defaultPrevented ||
      event.repeat ||
      event.ctrlKey ||
      event.metaKey ||
      event.altKey ||
      event.key.toLowerCase() !== 'c' ||
      (event.target instanceof Element &&
        event.target.closest('input, textarea, select, [contenteditable]'))
    )
      return;
    event.preventDefault();
    void copy();
  }
</script>

<svelte:window onkeydown={keydown} />

{#if available}
  <div class="session-copy">
    <span role="status">{notice}</span>
    <button
      class:flashed
      disabled={busy}
      onclick={copy}
      aria-label="Copy text"
      aria-keyshortcuts="C">[ (C)OPY ]</button
    >
  </div>
{/if}

<style>
  .session-copy {
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: 8px;
    margin-top: auto;
    padding-top: 6px;
  }
  span {
    font-size: 12px;
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  button {
    flex-shrink: 0;
    color: var(--accent);
    padding: 3px 7px;
  }
  button:hover,
  button.flashed {
    color: var(--bg);
    background: var(--accent);
  }
</style>
