<script lang="ts">
  import type { SavedPage } from './state.svelte';
  let { turns }: { turns?: SavedPage['turns'] } = $props();
</script>

{#each [...(turns || [])].reverse() as turn}
  {#if turn.task}<p class="muted">{turn.task}</p>{/if}
  <p>{turn.restricted ? 'View this reply in Codex.' : turn.reply}</p>
{:else}<p>No completed turns available.</p>{/each}

<style>
  p {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
