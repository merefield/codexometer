<script lang="ts">
  import { onMount } from 'svelte';
  import {
    inspectSession,
    sessionInspector,
    type AgentInspection,
  } from './state.svelte';
  let { root }: { root: string } = $props();
  let page = $state<AgentInspection>({});
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
      const result = await inspectSession(root, id, controller.signal);
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

<section class="panel" aria-label="Linked agents" data-session-inspector>
  <div class="spread">
    <h2>{child ? 'Agent transcript' : 'Linked agents'} // {root.slice(-5)}</h2>
    <button
      type="button"
      bind:this={close}
      onclick={() => (child ? void load('') : (sessionInspector.root = ''))}
      >{child ? 'Back to agents' : 'Close'}</button
    >
  </div>
  {#if busy}<p role="status">Loading…</p>
  {:else if failed}<p role="status">Session information unavailable.</p>
  {:else if child}
    {#each [...(page.turns || [])].reverse() as turn}
      {#if turn.task}<p class="muted transcript">{turn.task}</p>{/if}
      <p class="transcript">
        {turn.restricted ? 'View this reply in Codex.' : turn.reply}
      </p>
    {:else}<p>No completed turns available.</p>{/each}
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
