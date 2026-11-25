<script lang="ts">
  import SessionProse from './SessionProse.svelte';
  import type { SavedPage } from './state.svelte';
  let {
    turns,
    unverified = false,
  }: { turns?: SavedPage['turns']; unverified?: boolean } = $props();
</script>

{#each [...(turns || [])].reverse() as turn}
  {#if turn.task}<p class="muted">{turn.task}</p>{/if}
  {#if turn.restricted}<p>View this reply in Codex.</p>
  {:else if turn.prose?.length}<SessionProse blocks={turn.prose} />
  {:else if turn.reply}<p>{turn.reply}</p>{/if}
{:else}<p>
    {unverified
      ? 'Could not verify which completed turns belong to this agent.'
      : 'No completed turns available.'}
  </p>{/each}

<style>
  p {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
