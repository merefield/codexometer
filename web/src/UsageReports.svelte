<script lang="ts">
  import type { UsageReports } from './state.svelte';
  import { date } from './state.svelte';
  import { usageDimensions } from './contracts.generated';
  let { reports, mode }: { reports?: UsageReports | null; mode: string } =
    $props();
  let dimension = $state(usageDimensions[0]);
  let offset = $state(0);
  let daily = $derived(reports?.daily);
  let plan = $derived(reports?.plan);
  let count = $derived(
    mode === 'breakdown'
      ? daily?.days?.length || 0
      : plan?.periods?.length || 0,
  );
  let position = $derived(Math.min(offset, Math.max(0, count - 1)));
  let day = $derived(daily?.days?.[(daily?.days?.length || 0) - 1 - position]);
  let period = $derived(plan?.periods?.[position]);
  let unit = $derived(mode === 'breakdown' ? daily?.units || '' : '%');
  let distribution = $derived(
    (mode === 'breakdown' ? day : period)?.views?.[dimension],
  );
  let rows = $derived(distribution?.rows || []);
  let total = $derived(distribution?.total);
  let level = $derived(distribution?.level || 'unknown');
  let peak = $derived(distribution?.peak || 1);
  const amount = (n: number | null | undefined) =>
    n == null
      ? 'UNKNOWN'
      : n.toLocaleString('en-GB', { maximumFractionDigits: 2 });
</script>

<div class="controls">
  <label
    >GROUP <select aria-label="Usage grouping" bind:value={dimension}>
      {#each usageDimensions as item}
        <option value={item}
          >{item.charAt(0).toUpperCase() + item.slice(1)}</option
        >
      {/each}
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
  <h2>{mode === 'breakdown' ? 'DAILY BREAKDOWN' : 'QUOTA WINDOWS'}</h2>
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
      <span class:quality-warning={!plan.coverage_complete}
        >{plan.coverage_complete
          ? 'Reported range complete'
          : 'Partial coverage'}</span
      >{#if plan.approximate}<span class="quality-warning">
          // APPROXIMATE</span
        >{/if} // Latest fetch {date(plan.fetchedAt)}
    </p>
    <p class="muted">
      Coverage starts {plan.coverage_start || 'unknown'} // Data as of {plan.data_as_of ||
        'unknown'}{#if plan.boundary_tolerance_seconds != null}
        // Boundary tolerance {plan.boundary_tolerance_seconds}s{/if}
    </p>
    {#if period}
      <h3>
        {period.window_minutes} MIN // {period.plan_type} //
        <span
          class:muted={level === 'unknown'}
          class:quota-near={level === 'near'}
          class:quota-full={level === 'full'}
          >USED {amount(total)}{total == null ? '' : '%'}</span
        >
      </h3>
      <p class="window-dates">{period.starts_at} → {period.ends_at}</p>
      {#if !period.accounting_complete}<p class="quality-warning">
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
      <div class="report-head">
        <span></span><span></span><span
          >{mode === 'breakdown' ? unit : 'QUOTA USED'}</span
        >
      </div>
      {#each rows as { name, value }, index}
        <div
          class="report-row"
          class:unknown={name.trim().toLowerCase() === 'unknown'}
          class:alternate={index % 2 === 1}
        >
          <span title={name}>{name}</span>
          <div class="report-track">
            <div
              class="report-fill"
              style:width={`${Math.max(0, (100 * value) / peak)}%`}
            ></div>
          </div>
          <span class="report-value"
            >{amount(value)}{mode === 'periods' ? '%' : ''}</span
          >
        </div>
      {/each}
    </div>
  {:else if mode === 'breakdown' && distribution?.knownEmpty}
    <p class="empty">No usage reported for this day.</p>
  {:else if (daily && mode === 'breakdown') || (plan && mode === 'periods')}
    <p class="empty">
      Selected breakdown unavailable; missing does not mean zero.
    </p>
  {/if}
</section>

<style>
  h3,
  .window-dates,
  .report-value {
    color: var(--accent);
    font-weight: 700;
  }
  .quality-warning,
  .quota-near {
    color: var(--approval);
  }
  .quota-full {
    color: #ff6680;
  }
  .alternate .report-fill {
    background:
      radial-gradient(circle, var(--panel) 0.7px, transparent 0.9px) 0 0 / 3px
        3px,
      var(--accent);
  }
  .unknown,
  .unknown .report-value {
    color: var(--muted);
  }
  .unknown .report-fill {
    background: var(--muted);
    opacity: 1;
  }
  .report-bars {
    display: grid;
    grid-template-columns: minmax(5rem, 1fr) minmax(2rem, 3fr) auto;
    gap: 0.6rem;
  }
  .report-row,
  .report-head {
    display: grid;
    grid-column: 1 / -1;
    grid-template-columns: subgrid;
    align-items: center;
    gap: 0.6rem;
  }
  .report-head {
    color: var(--muted);
    font-size: 0.85rem;
  }
  .report-head > span:last-child,
  .report-value {
    text-align: right;
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
