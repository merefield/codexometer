<script lang="ts">
  import { onMount } from 'svelte';
  import SessionTranscript from './SessionTranscript.svelte';
  import {
    inspectSession,
    sessionInspector,
    type SessionInspection,
    type InspectionKind,
  } from './state.svelte';
  let { root, kind }: { root: string; kind: InspectionKind } = $props();
  let page = $state<SessionInspection>({});
  let child = $state('');
  let busy = $state(false);
  let failed = $state(false);
  let request: AbortController | undefined;
  let close = $state<HTMLButtonElement>();
  async function load(id: string) {
    request?.abort();
    const controller = new AbortController();
    request = controller;
    child = id;
    page = {};
    failed = false;
    busy = true;
    try {
      if (!inspectSession) throw new Error();
      const result = await inspectSession(root, id, kind, controller.signal);
      if (!controller.signal.aborted) page = result;
    } catch {
      if (!controller.signal.aborted) failed = true;
    } finally {
      if (!controller.signal.aborted) busy = false;
    }
  }
  onMount(() => {
    close?.focus();
    void load('');
    return () => request?.abort();
  });
</script>

<section
  class="panel"
  aria-label={kind === 'terminals' ? 'Background terminals' : 'Linked agents'}
  data-session-inspector
>
  <div class="spread">
    <h2>
      {kind === 'terminals'
        ? 'Background terminals'
        : child
          ? 'Agent transcript'
          : 'Linked agents'} // {root.slice(-5)}
    </h2>
    {#if kind === 'terminals'}<button
        type="button"
        disabled={busy}
        onclick={() => void load('')}>Refresh</button
      >{/if}
    <button
      type="button"
      bind:this={close}
      onclick={() => (child ? void load('') : (sessionInspector.root = ''))}
      >{child ? 'Back to agents' : 'Close'}</button
    >
  </div>
  {#if busy}<p role="status">Loading…</p>
  {:else if failed}<p role="status">Session information unavailable.</p>
  {:else if kind === 'terminals'}
    {#each page.terminals || [] as process}
      <p class="muted">
        {process.id}{process.pid
          ? ` // PID ${process.pid}`
          : ''}{process.directory ? ` // ${process.directory}` : ''}
      </p>
      <p class="transcript"><code>{process.command}</code></p>
    {:else}<p>No background terminals reported.</p>{/each}
    {#if page.more}<p>Additional background terminals are not shown.</p>{/if}
  {:else if child}
    <SessionTranscript turns={page.turns} />
  {:else}
    {#each page.agents || [] as agent (agent.id)}
      <button type="button" onclick={() => void load(agent.id)}
        >{agent.name || agent.id.slice(-5)} // {agent.status}</button
      >
    {:else}<p>No linked agents observed.</p>{/each}
  {/if}
</section>

<style>
  .transcript {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
