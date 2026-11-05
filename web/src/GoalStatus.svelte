<script lang="ts">
  import type { SessionGoal } from './state.svelte';
  let { goal, stale = false }: { goal?: SessionGoal | null; stale?: boolean } =
    $props();
  const outdated = $derived(stale || !!goal?.stale);
</script>

{#if goal}
  <span
    class="goal-status"
    class:stale={outdated}
    class:pursuing={!outdated && goal.status === 'active'}
    class:blocked={!outdated &&
      ['blocked', 'usageLimited', 'budgetLimited'].includes(goal.status)}
    aria-label="Goal status"
    title={goal.objective +
      '\n' +
      goal.tokensUsed.toLocaleString() +
      (goal.tokenBudget === null
        ? ' tokens'
        : ' / ' + goal.tokenBudget.toLocaleString() + ' tokens')}
    >{outdated ? 'GOAL STALE' : goal.label}</span
  >
{/if}

<style>
  .goal-status {
    color: var(--accent);
    font-size: 0.8rem;
    font-weight: 700;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pursuing {
    color: #67d391;
  }
  .blocked {
    color: var(--approval);
  }
  .stale {
    color: var(--muted);
  }
</style>
