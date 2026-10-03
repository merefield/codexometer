<script lang="ts">
  import { live, date } from './state.svelte';
</script>

{#if live.data?.thresholds?.length}
  <p class="eyebrow">MODEL STEP POLICY // OBSERVED {date(live.data.quotaAt)}</p>
  {#if live.data.quotaError}<p class="notice">
      Quota refresh failed. Policy state is based on the last successful
      observation.
    </p>{/if}
  <section class="panel thresholds-panel">
    <h1>THRESHOLDS // {live.data.thresholds.length} CONFIGURED</h1>
    <p class="muted">
      The longest Codex quota window selects one active model profile. ASK
      creates a per-session review; AUTO applies the profile on the next
      eligible check.
    </p>
    <div class="threshold-list">
      {#each live.data.thresholds as threshold}
        <article
          class:active={threshold.state === 'ACTIVE'}
          class:next={threshold.state === 'NEXT'}
        >
          <div class="threshold-trigger">{threshold.threshold}%</div>
          <div>
            <strong>{threshold.model}</strong>
            <p>
              {threshold.effort.toUpperCase()} REASONING // {threshold.speed.toUpperCase()}
              SPEED
            </p>
          </div>
          <div class="threshold-mode">{threshold.mode}</div>
          <div class="threshold-state">
            {threshold.state}{threshold.state === 'NEXT'
              ? ` // ${threshold.remaining || 0} PP TO GO`
              : ''}
          </div>
        </article>
      {/each}
    </div>
  </section>
{:else}
  <p class="empty">No threshold-based model steps were configured at launch.</p>
{/if}
