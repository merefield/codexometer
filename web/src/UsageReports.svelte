<script lang="ts">
  import type { UsageReports } from './state.svelte';
  import { date } from './state.svelte';
  let { reports, mode }: { reports?: UsageReports; mode: string } = $props();
  let dimension = $state('surface');
  let offset = $state(0);
  let daily = $derived(reports?.daily);
  let plan = $derived(reports?.plan);
  let count = $derived(
    mode === 'breakdown' ? daily?.days.length || 0 : plan?.periods.length || 0,
  );
  let position = $derived(Math.min(offset, Math.max(0, count - 1)));
  let day = $derived(daily?.days[(daily?.days.length || 0) - 1 - position]);
  let period = $derived(plan?.periods[position]);
  let unit = $derived(mode === 'breakdown' ? daily?.units || '' : '%');
  let rows = $derived.by(() => {
    let values: Record<string, number> = Object.create(null);
    if (mode === 'breakdown') values = day?.groups[dimension] || {};
    else {
      const wireDimension =
        dimension === 'feature'
          ? 'thread_source'
          : dimension === 'task start'
            ? 'turn_trigger'
            : dimension;
      for (const group of period?.breakdowns || []) {
        if (group.dimension !== wireDimension) continue;
        for (const row of group.rows)
          values[row.key] = (values[row.key] || 0) + row.basis_points / 100;
      }
    }
    return Object.entries(values).sort(
      (a, b) => b[1] - a[1] || a[0].localeCompare(b[0]),
    );
  });
  let total = $derived(
    mode === 'breakdown'
      ? day?.total
      : period?.used_basis_points == null
        ? null
        : period.used_basis_points / 100,
  );
  let peak = $derived(
    Math.max(1, total || 0, ...rows.map(([, value]) => value)),
  );
  const amount = (n: number | null | undefined) =>
    n == null
      ? 'UNKNOWN'
      : n.toLocaleString('en-GB', { maximumFractionDigits: 2 });
</script>

<div class="controls">
  <label
    >GROUP <select aria-label="Usage grouping" bind:value={dimension}>
      <option value="surface">Surface</option><option value="model"
        >Model</option
      ><option value="feature">Feature</option><option value="task start"
        >Task start</option
      >
    </select></label
  >
  <button
    disabled={position >= count - 1}
    onclick={() => (offset = position + 1)}>← OLDER</button
  >
  <button disabled={position === 0} onclick={() => (offset = position - 1)}
    >NEWER →</button
  >
  <span class="muted">{count ? `${position + 1} / ${count}` : 'NO DATA'}</span>
</div>
<section class="panel">
  <h2>{mode === 'breakdown' ? 'DAILY BREAKDOWN' : 'ALLOWANCE PERIODS'}</h2>
  <p class="eyebrow">
    {mode === 'breakdown'
      ? reports?.dailyStatus || 'UNAVAILABLE'
      : reports?.planStatus || 'UNAVAILABLE'} // UTC
  </p>
  {#if mode === 'breakdown' && daily}
    <p class="muted">
      Latest fetch {date(daily.fetchedAt)} // queried {daily.from} → {daily.through}.
      Reports are fetched from OpenAI and are not saved between runs.
    </p>
    {#if daily.dataAsOf}<p class="muted">Data as of {daily.dataAsOf}</p>{/if}
    <h3>
      {day?.date || 'No daily buckets reported'} // {amount(total)}
      {unit}
    </h3>
    <p class="muted">
      Relative usage/credits are not tokens or quota percentages. Groupings
      describe the same usage, not additional consumption.
    </p>
  {:else if mode === 'periods' && plan}
    <p class="muted">
      {plan.coverage_complete
        ? 'Reported range complete'
        : 'Partial coverage'}{plan.approximate ? ' // APPROXIMATE' : ''} // Latest
      fetch {date(plan.fetchedAt)}
    </p>
    <p class="muted">
      Coverage starts {plan.coverage_start || 'unknown'} // Data as of {plan.data_as_of ||
        'unknown'}{#if plan.boundary_tolerance_seconds != null}
        // Boundary tolerance {plan.boundary_tolerance_seconds}s{/if}
    </p>
    {#if period}
      <h3>
        {period.window_minutes} MIN // {period.plan_type} // USED {amount(
          total,
        )}{total == null ? '' : '%'}
      </h3>
      <p>{period.starts_at} → {period.ends_at}</p>
      {#if !period.accounting_complete}<p class="notice">
          Partial accounting: totals may still change.
        </p>{/if}
      <p class="muted">
        Percentage of this period's historical allowance, not today's limit. May
        exceed 100%. This is not a manual/automatic reset log.
      </p>
    {/if}
  {:else}
    <p class="empty">
      This optional account report is unavailable. Token history remains
      available; missing data does not mean zero usage.
    </p>
  {/if}
  {#if rows.length}
    <div class="report-bars">
      {#each rows as [name, value]}
        <div class="report-row">
          <span title={name}>{name}</span>
          <div class="report-track">
            <div
              class="report-fill"
              style:width={`${Math.max(0, (100 * value) / peak)}%`}
            ></div>
          </div>
          <span>{amount(value)} {unit}</span>
        </div>
      {/each}
    </div>
  {:else if mode === 'breakdown' && day?.total === 0 && day?.groups[dimension] != null}
    <p class="empty">No usage reported for this day.</p>
  {:else if (daily && mode === 'breakdown') || (plan && mode === 'periods')}
    <p class="empty">
      Selected breakdown unavailable; missing does not mean zero.
    </p>
  {/if}
</section>

<style>
  .report-bars {
    display: grid;
    gap: 0.6rem;
  }
  .report-row {
    display: grid;
    grid-template-columns: minmax(5rem, 1fr) minmax(2rem, 3fr) auto;
    align-items: center;
    gap: 0.6rem;
  }
  .report-row > span:first-child {
    overflow-wrap: anywhere;
  }
  .report-track {
    height: 1rem;
    background: var(--panel);
  }
  .report-fill {
    height: 100%;
    background: var(--accent);
  }
</style>
