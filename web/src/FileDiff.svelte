<script lang="ts">
  import type { FileDiffLine } from './state.svelte';
  let { lines }: { lines: FileDiffLine[] } = $props();
</script>

<div class="file-diff" aria-label="Proposed file changes">
  {#each lines as line}
    <div
      class:added={line.kind === 'addition'}
      class:removed={line.kind === 'removal'}
      class:heading={line.kind === 'heading'}
      class="diff-line"
    >
      {#if line.kind !== 'heading' && line.kind !== 'metadata'}<span
          class="line-number">{line.old || ''}</span
        ><span class="line-number">{line.new || ''}</span>{/if}<code
        >{line.text}</code
      >
    </div>
  {/each}
</div>

<style>
  .file-diff {
    overflow: auto;
    max-height: 60vh;
    font-family: monospace;
    border: 1px solid var(--muted);
    padding: 0.5rem;
  }
  .diff-line {
    display: flex;
    white-space: pre;
    min-height: 1.3em;
  }
  .line-number {
    display: inline-block;
    flex: 0 0 5ch;
    text-align: right;
    padding-right: 1ch;
    opacity: 0.75;
    user-select: none;
  }
  .added {
    color: #67d391;
    background: #67d39112;
  }
  .removed {
    color: #ff6b83;
    background: #ff6b8312;
  }
  .heading {
    font-weight: bold;
    padding: 0.5rem 0;
  }
</style>
