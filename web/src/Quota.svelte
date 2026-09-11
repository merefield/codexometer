<script lang="ts">
  import { onMount } from 'svelte';
  import { live, date } from './state.svelte';
  import type { Meter } from './state.svelte';
  let { params = {} }: { params?: { view?: string } } = $props();
  const views = ['bars', 'pace', 'pie', 'fuel', 'resets'];
  let now = $state(Date.now());
  let view = $derived(
    views.includes(params.view || '') ? params.view! : 'bars',
  );
  onMount(() => {
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });
  function elapsed(m: Meter): number | null {
    if (!m.duration || m.duration <= 0 || !m.reset) return null;
    return Math.max(
      0,
      Math.min(100, 100 * (1 - (m.reset * 1000 - now) / (m.duration * 60000))),
    );
  }
  function sector(used: number): string {
    const angle = (used / 100) * Math.PI * 2;
    return `M60 60 L60 14 A46 46 0 ${used > 50 ? 1 : 0} 1 ${60 + 46 * Math.sin(angle)} ${60 - 46 * Math.cos(angle)} Z`;
  }
</script>

<nav class="secondary" aria-label="Quota view">
  {#each views as item}<a
      href={'#/quota/' + item}
      class:active={view === item}
      aria-current={view === item ? 'page' : undefined}
      >{item === 'pace'
        ? 'CONSUMPTION PACE'
        : item === 'fuel'
          ? 'FUEL TANK'
          : item.toUpperCase()}</a
    >{/each}
</nav>
{#if live.data}
  {#if live.data.quotaError}<p class="notice">
      Quota refresh failed. Values below are the last successful observation.
    </p>{/if}
  <p class="eyebrow">QUOTA // OBSERVED {date(live.data.quotaAt)}</p>
  {#if view === 'resets'}
    <section class="panel">
      <h1>RESET INVENTORY // {live.data.creditCount} AVAILABLE</h1>
      <p>Read-only preview. Use the terminal to redeem a reset.</p>
      {#if !live.data.credits.length}<p>
          Expiry details unavailable. No listed expiry does not mean no expiry.
        </p>{/if}
      {#each live.data.credits as credit}<article class="credit">
          <h2>{credit.title || 'Quota reset'} // {credit.status}</h2>
          <p>
            {!credit.expiryKnown
              ? 'Expiry information unavailable'
              : credit.expires
                ? 'EXPIRES ' + date(credit.expires)
                : 'Does not expire'}
          </p>
        </article>{/each}
      <p class="muted">
        The backend may return only some credits. This list does not establish
        redemption order.
      </p>
    </section>
  {:else}
    <div class:radial={view === 'pie'} class="quota-grid">
      {#each live.data.meters as meter}
        {@const cycle = elapsed(meter)}
        {@const pace = cycle === null ? null : cycle - meter.used}
        <section class="panel quota-card">
          <h2>{meter.name}</h2>
          <div class="spread">
            {#if view === 'fuel'}<span>FREE {100 - meter.used}%</span><span
                class="muted">USED {meter.used}%</span
              >{:else}<span>USED {meter.used}%</span><span class="muted"
                >FREE {100 - meter.used}%</span
              >{/if}
          </div>
          {#if view === 'pie'}
            <div class="pie-wrap">
              <svg
                viewBox="0 0 120 120"
                role="img"
                aria-label={`${meter.used}% quota used`}
              >
                <circle class="pie-base" cx="60" cy="60" r="46" />
                {#if meter.used >= 100}<circle
                    class="pie-fill"
                    cx="60"
                    cy="60"
                    r="46"
                  />{:else if meter.used > 0}<path
                    class="pie-fill"
                    d={sector(meter.used)}
                  />{/if}
              </svg>
            </div>
          {:else if view === 'pace'}
            {#if pace !== null}
              <div class="pace">
                <div class="pace-mid"></div>
                <span class="pace-marker" style:left={`${(pace + 100) / 2}%`}
                  >▼</span
                >
              </div>
              <div class="spread muted">
                <span>−100 // AHEAD OF BUDGET</span><span>+100 // HEADROOM</span
                >
              </div>
              <p class="readout">
                {pace >= 0 ? '+' : ''}{pace.toFixed(1)} PP
                <small
                  >{pace >= 0 ? 'WITHIN PACE' : 'USING FASTER THAN TIME'}</small
                >
              </p>
            {:else}<p class="empty">
                Cycle duration unavailable — pace cannot be calculated.
              </p>{/if}
          {:else}
            <div
              class="gauge"
              role="meter"
              aria-label={view === 'fuel' ? 'Fuel remaining' : 'Quota used'}
              aria-valuenow={view === 'fuel' ? 100 - meter.used : meter.used}
              aria-valuemin="0"
              aria-valuemax="100"
            >
              <div
                style:width={`${view === 'fuel' ? 100 - meter.used : meter.used}%`}
              ></div>
            </div>
            {#if view === 'fuel'}<div class="spread muted">
                <span>EMPTY</span><span>FULL</span>
              </div>{/if}
            {#if cycle !== null}<p class="eyebrow">
                RESET CYCLE // {Math.floor(cycle)}% ELAPSED
              </p>
              <div class="gauge timeline">
                <div
                  style:width={`${view === 'fuel' ? 100 - cycle : cycle}%`}
                ></div>
              </div>{/if}
          {/if}
          <p class="muted">RESETS // {date(meter.reset)}</p>
          {#if meter.details}<p>{meter.details}</p>{/if}
        </section>
      {/each}
    </div>
    {#if !live.data.meters.length}<p class="empty">
        No quota windows reported yet.
      </p>{/if}
  {/if}
  <p class="muted">
    All reported windows are shown. API-equivalent learning and quota status
    scoring remain in the terminal for this first preview.
  </p>
{/if}
