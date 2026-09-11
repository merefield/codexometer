<script lang="ts">
  import { live, date, number } from './state.svelte';
  import Graph from './Graph.svelte';
  let mode = $state('daily');
  let months = $state(12);
  let offset = $state(0);
  let days = $derived.by(() => {
    const buckets = live.data?.usage?.dailyUsageBuckets;
    if (!buckets) return [];
    const end = new Date();
    end.setUTCHours(0, 0, 0, 0);
    end.setUTCMonth(end.getUTCMonth() - offset * months);
    const start = new Date(end);
    start.setUTCMonth(start.getUTCMonth() - months);
    start.setUTCDate(start.getUTCDate() + 1);
    const lookup = new Map<string, number>();
    for (const bucket of buckets) {
      if (!Number.isFinite(bucket.tokens) || bucket.tokens < 0) continue;
      lookup.set(
        bucket.startDate,
        (lookup.get(bucket.startDate) || 0) + bucket.tokens,
      );
    }
    const result: { date: string; tokens: number }[] = [];
    for (
      let day = start;
      day <= end;
      day = new Date(day.getTime() + 86400000)
    ) {
      const date = day.toISOString().slice(0, 10);
      result.push({ date, tokens: lookup.get(date) || 0 });
    }
    return result;
  });
  let peak = $derived(Math.max(1, ...days.map((d) => d.tokens)));
  let leading = $derived(
    days.length ? new Date(days[0].date + 'T00:00:00Z').getUTCDay() : 0,
  );
  let bars = $derived.by(() => {
    if (mode === 'monthly') {
      const grouped = new Map<string, number>();
      for (const day of days)
        grouped.set(
          day.date.slice(0, 7),
          (grouped.get(day.date.slice(0, 7)) || 0) + day.tokens,
        );
      return [...grouped].map(([date, tokens]) => ({ date, tokens }));
    }
    let total = 0;
    return days.map((day) => ({
      date: day.date,
      tokens: (total += day.tokens),
    }));
  });
</script>

<h1>USAGE // ACCOUNT HISTORY</h1>
<p class="muted">
  Account-wide history reported by Codex, not the local Sessions counter. Dates
  use UTC. Historical resets are not provided by this data.
</p>
<div class="controls">
  <label
    >VIEW <select aria-label="Usage view" bind:value={mode}
      ><option value="daily">Daily heatmap</option><option value="monthly"
        >Monthly bars</option
      ><option value="cumulative">Cumulative bars</option></select
    ></label
  ><label
    >PERIOD <select
      aria-label="Usage period"
      bind:value={months}
      onchange={() => (offset = 0)}
      ><option value={6}>6 months</option><option value={12}>12 months</option
      ></select
    ></label
  ><button onclick={() => offset++}>← EARLIER</button><button
    disabled={offset === 0}
    onclick={() => offset--}>LATER →</button
  >
</div>
{#if live.data?.usageError}<p class="notice">
    History refresh failed. Any displayed history is the last successful
    observation.
  </p>{/if}
{#if live.data?.usage && live.data.usage.dailyUsageBuckets !== null}
  <div class="summary-grid">
    <section class="panel">
      <h2>LIFETIME TOKENS</h2>
      <p class="readout">{number(live.data.usage.summary.lifetimeTokens)}</p>
    </section>
    <section class="panel">
      <h2>PEAK DAY</h2>
      <p class="readout">{number(live.data.usage.summary.peakDailyTokens)}</p>
    </section>
    <section class="panel">
      <h2>CURRENT STREAK</h2>
      <p class="readout">
        {number(live.data.usage.summary.currentStreakDays)} <small>DAYS</small>
      </p>
    </section>
  </div>
  <section class="panel">
    <h2>{days[0]?.date} — {days.at(-1)?.date}</h2>
    {#if mode === 'daily'}<div class="heat-scroll">
        <div
          class="heatmap"
          role="img"
          aria-label="Daily token usage heatmap, brighter cells indicate more tokens"
        >
          {#each Array(leading) as _}<div aria-hidden="true"></div>{/each}
          {#each days as day}<div
              class="heat-cell"
              class:zero={day.tokens === 0}
              style:opacity={day.tokens ? 0.25 + (0.75 * day.tokens) / peak : 1}
              title={`${day.date}: ${number(day.tokens)} tokens`}
            ></div>{/each}
        </div>
      </div>
      <p class="muted">LESS ░ ▒ ▓ █ MORE // HOVER FOR DATE AND TOKENS</p>
    {:else}<Graph values={bars.map((b) => b.tokens)} label={mode + ' usage'} />
      <div class="spread muted">
        <span>{bars[0]?.date}</span><span>{bars.at(-1)?.date}</span>
      </div>{/if}
    <details>
      <summary>Accessible data table</summary>
      <div class="table-scroll">
        <table>
          <thead><tr><th>Date (UTC)</th><th>Tokens</th></tr></thead><tbody
            >{#each mode === 'daily' ? days : bars as row}<tr
                ><td>{row.date}</td><td>{number(row.tokens)}</td></tr
              >{/each}</tbody
          >
        </table>
      </div>
    </details>
  </section>
  <p class="eyebrow">
    OBSERVED {date(live.data.usageAt)} // CUMULATIVE IS FOR THE SELECTED PERIOD
  </p>
{:else}<p class="empty">
    History unavailable or awaiting a matching account observation. Missing
    history is not treated as zero usage.
  </p>{/if}
