<script module lang="ts">
  import type { ProseBlock, ProseRun } from './contracts.generated';

  const align = (value?: string) =>
    value === 'right' || value === 'center' ? value : 'left';

  function inlineGroups(runs?: ProseRun[] | null) {
    const groups: ProseRun[][] = [];
    for (const run of runs || []) {
      const last = groups.at(-1);
      if (!last || run.threadStart || last[0].threadId !== run.threadId)
        groups.push([run]);
      else last.push(run);
    }
    return groups;
  }
</script>

<script lang="ts">
  import { openSessionLink } from './state.svelte';
  let { blocks }: { blocks: ProseBlock[] } = $props();
</script>

{#snippet literal(r: ProseRun)}
  {#if r.code}<code>{r.text}</code>{:else}{r.text}{/if}
{/snippet}
{#snippet emphasis(r: ProseRun)}
  {#if r.italic}<em>{@render literal(r)}</em>{:else}{@render literal(r)}{/if}
{/snippet}
{#snippet label(group: ProseRun[])}
  {#each group as r}{#if r.bold}<strong>{@render emphasis(r)}</strong
      >{:else}{@render emphasis(r)}{/if}{/each}
{/snippet}
{#snippet inline(runs: ProseRun[] | null | undefined)}
  {#each inlineGroups(runs) as group}
    {@const id = group[0].threadId}
    {#if id}<button
        class="thread-link"
        type="button"
        onclick={() => openSessionLink(id)}>{@render label(group)}</button
      >{:else}{@render label(group)}{/if}
  {/each}
{/snippet}

<div class="prose" data-reply-text>
  {#each blocks as block}
    {#if block.rows?.length}
      <div class="table-scroll">
        <table>
          <thead
            ><tr
              >{#each block.rows[0] || [] as cell, j}<th
                  scope="col"
                  style:text-align={align(block.align?.[j])}
                  >{@render inline(cell)}</th
                >{/each}</tr
            ></thead
          >
          <tbody>
            {#each block.rows.slice(1) as row}
              <tr
                >{#each row || [] as cell, j}<td
                    style:text-align={align(block.align?.[j])}
                    >{@render inline(cell)}</td
                  >{/each}</tr
              >
            {/each}
          </tbody>
        </table>
      </div>
    {:else}<div class="text">{@render inline(block.runs)}</div>{/if}
  {/each}
</div>

<style>
  .thread-link {
    display: inline;
    border: 0;
    padding: 0;
    background: transparent;
    color: var(--accent);
    font: inherit;
    text-align: inherit;
    text-decoration: underline;
    text-transform: none;
    overflow-wrap: anywhere;
  }
  .prose {
    min-width: 0;
    max-width: 100%;
    font-size: 13px;
    line-height: 1.5;
  }
  .text,
  td,
  th {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  strong,
  th {
    font-weight: 700;
    color: var(--accent);
  }
  code {
    font-family: inherit;
    background: color-mix(in srgb, var(--edge) 65%, var(--bg));
  }
  .table-scroll {
    max-width: 100%;
    overflow-x: auto;
  }
  :global(.session-row .context) .prose {
    max-height: 230px;
    overflow: auto;
  }
  :global(.session-row.selected .context) .prose {
    max-height: 460px;
  }
  table {
    border-collapse: collapse;
  }
  th,
  td {
    padding: 0.12em 0.75em;
    vertical-align: top;
    min-width: 5ch;
  }
  th:first-child,
  td:first-child {
    padding-inline-start: 0;
  }
  th {
    border-bottom: 1px solid var(--muted);
  }
</style>
