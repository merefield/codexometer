<script lang="ts">
  import type { Meter } from './state.svelte';
  import { date } from './state.svelte';
  let {
    used,
    elapsed,
    trail = [],
    duration,
  }: {
    used: number;
    elapsed: number;
    trail?: Meter['trail'];
    duration: number | null;
  } = $props();
  type TrailPoint = NonNullable<Meter['trail']>[number];
  type TrendMode = 'off' | 'halfHour' | 'hour' | 'day' | 'window';
  const gradient = $props.id();
  let showObservations = $state(false);
  let showTrace = $state(true);
  let graphMode = $state<'consumption' | 'pace'>('consumption');
  let trendMode = $state<TrendMode>('window');
  const ticks = [0, 25, 50, 75, 100];
  const trendMinutes = { off: 0, halfHour: 30, hour: 60, day: 1440, window: 0 };
  let paceView = $derived(graphMode === 'pace');
  let yTicks = $derived(paceView ? [-100, -50, 0, 50, 100] : ticks);
  let minimum = $derived(paceView ? -100 : 0);
  let range = $derived(paceView ? 200 : 100);
  let width = $state(400);
  let height = $state(240);
  let right = $derived(width - 24);
  let bottom = $derived(height - 48);
  let plotWidth = $derived(Math.max(1, right - 48));
  let plotHeight = $derived(Math.max(1, bottom - 20));
  let x = $derived(
    48 + (Math.max(0, Math.min(100, elapsed)) / 100) * plotWidth,
  );
  function plotY(value: number): number {
    return bottom - ((value - minimum) / range) * plotHeight;
  }
  function graphValue(consumption: number, time: number): number {
    return paceView ? consumption - time : consumption;
  }
  let y = $derived(plotY(graphValue(used, elapsed)));
  let difference = $derived(used - elapsed);
  let path = $derived(
    trail
      .map(
        (p, i) =>
          `${i === 0 || p.break ? 'M' : 'L'}${48 + (p.elapsed / 100) * plotWidth} ${plotY(graphValue(p.used, p.elapsed))}`,
      )
      .join(' '),
  );
  let continuous = $derived.by(() => {
    let start = 0;
    for (let i = trail.length - 1; i > 0; i--) {
      if (trail[i].break) {
        start = i;
        break;
      }
    }
    return trail.slice(start);
  });
  let observedMilliseconds = $derived.by(() => {
    if (continuous.length < 2) return 0;
    return Math.max(
      0,
      Date.parse(continuous.at(-1)!.at) - Date.parse(continuous[0].at),
    );
  });
  function observedPeriod(span: number): boolean {
    if (observedMilliseconds < span || continuous.length < 2) return false;
    const cutoff = Date.parse(continuous.at(-1)!.at) - span;
    return (
      continuous.filter((point) => Date.parse(point.at) >= cutoff).length >= 2
    );
  }
  let availability = $derived({
    off: true,
    halfHour: observedPeriod(30 * 60_000),
    hour: observedPeriod(60 * 60_000),
    day: observedPeriod(24 * 60 * 60_000),
    window: Number.isFinite(elapsed) && elapsed > 0 && Number.isFinite(used),
  });
  $effect(() => {
    if (trendMode !== 'window' && !availability[trendMode])
      trendMode = 'window';
  });
  function trendPoints(mode: TrendMode): TrailPoint[] {
    if (mode === 'off' || !availability[mode]) return [];
    const span = trendMinutes[mode] * 60_000;
    const cutoff = Date.parse(continuous.at(-1)!.at) - span;
    const start = continuous.findIndex(
      (point) => Date.parse(point.at) >= cutoff,
    );
    if (start < 0) return [];
    return continuous.slice(start);
  }
  function regressionSlope(points: TrailPoint[]): number | null {
    if (points.length < 2) return null;
    const xMean =
      points.reduce((sum, point) => sum + point.elapsed, 0) / points.length;
    const yMean =
      points.reduce((sum, point) => sum + point.used, 0) / points.length;
    let covariance = 0;
    let variance = 0;
    for (const point of points) {
      covariance += (point.elapsed - xMean) * (point.used - yMean);
      variance += (point.elapsed - xMean) ** 2;
    }
    return variance > 0 ? Math.max(0, covariance / variance) : null;
  }
  let trend = $derived.by(() => {
    const slope =
      trendMode === 'window' && availability.window
        ? used / elapsed
        : regressionSlope(trendPoints(trendMode));
    if (slope === null || !duration) return null;
    const projected = used + slope * (100 - elapsed);
    const intercept = trendMode === 'window' ? 0 : used - slope * elapsed;
    const graphSlope = slope - (paceView ? 1 : 0);
    let x1 = Math.max(0, elapsed - (trendMinutes[trendMode] / duration) * 100);
    if (trendMode === 'window') x1 = 0;
    let x2 = 100;
    if (graphSlope !== 0) {
      const lower = (minimum - intercept) / graphSlope;
      const upper = (minimum + range - intercept) / graphSlope;
      x1 = Math.max(x1, Math.min(lower, upper));
      x2 = Math.min(100, Math.max(lower, upper));
    }
    if (x1 > x2) return null;
    const y1 = intercept + graphSlope * x1;
    const y2 = intercept + graphSlope * x2;
    const pixelLength = Math.hypot(
      ((x2 - x1) / 100) * plotWidth,
      ((y2 - y1) / range) * plotHeight,
    );
    const vertices = Math.max(3, Math.min(9, Math.floor(pixelLength / 65) + 2));
    const points = Array.from({ length: vertices }, (_, index) => {
      const px = x1 + ((x2 - x1) * index) / (vertices - 1);
      const py = y1 + ((y2 - y1) * index) / (vertices - 1);
      return `${48 + (px / 100) * plotWidth} ${plotY(py)}`;
    });
    return {
      path: `M${points.join(' L')}`,
      projected,
      safe: projected <= 100,
    };
  });
  let observedLabel = $derived.by(() => {
    if (continuous.length < 2) return 'ONE OBSERVATION';
    const minutes = Math.floor(observedMilliseconds / 60_000);
    if (minutes < 60) return `${minutes} MIN CONTINUOUS`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}H CONTINUOUS`;
    return `${Math.floor(hours / 24)}D ${hours % 24}H CONTINUOUS`;
  });
</script>

<div class="zone-modes" role="group" aria-label="Style">
  <span class="muted">Style:</span>
  <label
    ><input
      type="radio"
      name={gradient + '-mode'}
      value="consumption"
      bind:group={graphMode}
    /> Consumption</label
  >
  <label
    ><input
      type="radio"
      name={gradient + '-mode'}
      value="pace"
      bind:group={graphMode}
    /> Pace</label
  >
</div>
<div class="zone-canvas" bind:clientWidth={width} bind:clientHeight={height}>
  <svg
    class="consumption-zone"
    style:--trend-color={trend?.safe ? '#115b35' : '#7f1825'}
    viewBox={`0 0 ${width} ${height}`}
    role="img"
    aria-label={`${paceView ? 'Pace zone' : 'Consumption zone'}: ${used}% consumed, ${elapsed.toFixed(1)}% of quota period elapsed. ${paceView ? `${difference.toFixed(1)} percentage points from safety.` : `${difference > 0 ? 'Above' : difference < 0 ? 'Below' : 'On'} the steady-consumption line.`}${showTrace && trail.length ? ` Trace path contains ${trail.length} ${trail.length === 1 ? 'observation' : 'observations'}; gaps are not interpolated.` : ''}`}
  >
    <defs>
      <linearGradient
        id={gradient}
        x1="0%"
        y1="0%"
        x2={paceView ? '0%' : '100%'}
        y2="100%"
      >
        <stop offset="0%" stop-color="#b93749" />
        {#if paceView}<stop offset="25%" stop-color="#bf6f32" />{/if}
        <stop offset="50%" stop-color={paceView ? '#ae903b' : '#8c793d'} />
        <stop offset="100%" stop-color="#23855d" />
      </linearGradient>
      <marker
        id={gradient + '-trend-arrow'}
        viewBox="0 0 8 8"
        refX="5"
        refY="4"
        markerWidth="14"
        markerHeight="14"
        orient="auto"
        markerUnits="userSpaceOnUse"
      >
        <path class="trend-arrow" d="M1 1 L6 4 L1 7 Z" />
      </marker>
    </defs>
    <rect
      class="zone-field"
      x="48"
      y="20"
      width={plotWidth}
      height={plotHeight}
      fill={`url(#${gradient})`}
    />
    {#each ticks as tick}
      <line
        class="grid"
        x1={48 + (tick / 100) * plotWidth}
        y1="20"
        x2={48 + (tick / 100) * plotWidth}
        y2={bottom}
      />
      <text
        x={48 + (tick / 100) * plotWidth}
        y={bottom + 20}
        text-anchor="middle">{tick}%</text
      >
    {/each}
    {#each yTicks as tick}
      <line class="grid" x1="48" y1={plotY(tick)} x2={right} y2={plotY(tick)} />
      <text x="40" y={plotY(tick) + 4} text-anchor="end"
        >{paceView ? `${tick > 0 ? '+' : ''}${tick}` : `${tick}%`}</text
      >
    {/each}
    <path class="axes" d={`M48 20 V${bottom} H${right}`} />
    <line
      class="pace-line"
      x1="48"
      y1={plotY(0)}
      x2={right}
      y2={paceView ? plotY(0) : plotY(100)}
    />
    {#if paceView}<text
        class="safe-label"
        x={right - 5}
        y={plotY(0) - 6}
        text-anchor="end">SAFE</text
      >{/if}
    {#if showTrace && trail.length}
      <path class="observation-trail-casing" d={path} />
      <path class="observation-trail" d={path} />
      <circle
        class="trail-start"
        cx={48 + (trail[0].elapsed / 100) * plotWidth}
        cy={plotY(graphValue(trail[0].used, trail[0].elapsed))}
        r="5"><title>First observation: {date(trail[0].at)}</title></circle
      >
    {/if}
    {#if trend}
      <path class="trend-casing" d={trend.path} />
      <path
        class="trend-line"
        d={trend.path}
        marker-mid={`url(#${gradient}-trend-arrow)`}
        ><title
          >{trendMode === 'window'
            ? 'Window-average pace'
            : 'Recent consumption trajectory'} projects {trend.projected.toFixed(
            1,
          )}% at reset if continued</title
        ></path
      >
    {/if}
    <text x="48" y="12" class="axis-title"
      >{paceView ? 'DISTANCE FROM SAFETY (PP)' : 'CONSUMPTION'}</text
    >
    <text
      x={48 + plotWidth / 2}
      y={height - 5}
      text-anchor="middle"
      class="axis-title">QUOTA PERIOD ELAPSED</text
    >
    <circle class="position-halo" cx={x} cy={y} r="10" />
    <circle class="position-dot" cx={x} cy={y} r="5">
      <title
        >{used}% consumed // {elapsed.toFixed(1)}% of period elapsed{paceView
          ? ` // ${difference.toFixed(1)} PP from safety`
          : ''}</title
      >
    </circle>
  </svg>
</div>
<div class="zone-footer">
  <p class="zone-caption">
    {#if paceView}{difference > 0 ? '+' : ''}{difference.toFixed(1)} PP FROM SAFETY{:else}{used}%
      USED{/if} // {elapsed.toFixed(1)}% TIME ELAPSED<br />
    <span class="muted"
      >{Math.abs(difference) < 0.05
        ? 'ON PACE'
        : difference > 0
          ? paceView
            ? 'ABOVE SAFETY — CONSUMPTION AHEAD OF TIME'
            : 'ABOVE THE LINE — CONSUMING FASTER THAN TIME'
          : paceView
            ? 'BELOW SAFETY — QUOTA HEADROOM'
            : 'BELOW THE LINE — WITHIN PACE'}</span
    >
    {#if trend}<br /><span class="trend-summary"
        >TREND // {paceView
          ? `${trend.projected - 100 > 0 ? '+' : ''}${(trend.projected - 100).toFixed(1)} PP AT RESET`
          : trend.projected >= 100
            ? 'QUOTA EXHAUSTION PROJECTED'
            : `${trend.projected.toFixed(1)}% PROJECTED AT RESET`}</span
      >{/if}
  </p>
  <div class="zone-controls">
    <label class="trace-control"
      ><input type="checkbox" bind:checked={showTrace} /> TRACE PATH</label
    >
    <label class="trend-control"
      ><span>TREND //</span>
      <select aria-label="Trend period" bind:value={trendMode}>
        <option value="off">OFF</option>
        <option value="halfHour" disabled={!availability.halfHour}
          >LAST 30 MINUTES{availability.halfHour
            ? ''
            : ' — UNAVAILABLE'}</option
        >
        <option value="hour" disabled={!availability.hour}
          >LAST HOUR{availability.hour ? '' : ' — UNAVAILABLE'}</option
        >
        <option value="day" disabled={!availability.day}
          >LAST 24 HOURS{availability.day ? '' : ' — UNAVAILABLE'}</option
        >
        <option value="window" disabled={!availability.window}
          >FROM WINDOW START{availability.window
            ? ''
            : ' — NO TIME ELAPSED'}</option
        >
      </select></label
    >
    <small>HISTORY // {observedLabel}</small>
  </div>
</div>
{#if trail.length}<details
    class="observation-details"
    bind:open={showObservations}
  >
    <summary>OBSERVATION DATA</summary>
    {#if showObservations}<div class="observation-table">
        <table>
          <caption>Quota observations</caption>
          <thead
            ><tr
              ><th scope="col">Observed at</th><th scope="col"
                >Period elapsed</th
              ><th scope="col">Consumed</th><th scope="col">Trail segment</th
              ></tr
            ></thead
          >
          <tbody
            >{#each trail as point, index}<tr>
                <td><time datetime={point.at}>{date(point.at)}</time></td>
                <td>{point.elapsed.toFixed(1)}%</td><td>{point.used}%</td>
                <td
                  >{index === 0
                    ? 'First observation'
                    : point.break
                      ? 'Gap before this observation'
                      : 'Connected to previous observation'}</td
                >
              </tr>{/each}</tbody
          >
        </table>
      </div>{/if}
  </details>{/if}

<style>
  .zone-modes {
    display: flex;
    align-self: center;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
    border: 1px solid var(--edge);
    border-radius: 4px;
    padding: 5px 10px;
    font-size: 12px;
  }
  .zone-modes label {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    cursor: pointer;
    color: var(--ink);
  }
  .zone-modes label:hover {
    color: var(--accent);
  }
  .zone-modes input {
    margin: 0;
    accent-color: var(--accent);
  }
  .observation-details {
    font-size: 12px;
  }
  summary {
    cursor: pointer;
    color: var(--accent);
  }
  .observation-table {
    overflow: auto;
    max-height: 240px;
  }
  .zone-canvas {
    position: relative;
    flex: 1;
    min-height: 170px;
    margin-top: 6px;
  }
  svg {
    position: absolute;
    inset: 0;
    display: block;
    width: 100%;
    height: 100%;
  }
  text {
    fill: var(--ink);
    font:
      14px 'Cascadia Code',
      Consolas,
      monospace;
  }
  .axis-title {
    font-size: 12px;
    letter-spacing: 0.04em;
  }
  .safe-label {
    fill: #fff;
    font-size: 11px;
  }
  .grid {
    stroke: #fff;
    stroke-opacity: 0.14;
    stroke-width: 1;
  }
  .axes {
    stroke: var(--ink);
    stroke-width: 1.5;
    fill: none;
  }
  .pace-line {
    stroke: #fff;
    stroke-width: 2;
    stroke-dasharray: 6 4;
  }
  .position-halo {
    fill: #101820;
    stroke: #fff;
    stroke-width: 1.5;
  }
  .position-dot {
    fill: #fff;
  }
  .observation-trail {
    fill: none;
    stroke: #101820;
    stroke-width: 2.5;
    stroke-linejoin: round;
  }
  .observation-trail-casing {
    fill: none;
    stroke: rgb(255 255 255 / 45%);
    stroke-width: 4.5;
    stroke-linejoin: round;
  }
  .trail-start {
    fill: #101820;
    stroke: rgb(255 255 255 / 70%);
    stroke-width: 2;
  }
  .trend-line {
    fill: none;
    stroke: var(--trend-color);
    stroke-width: 2;
    stroke-dasharray: 3 6;
  }
  .trend-arrow {
    fill: var(--trend-color);
    stroke: rgb(255 255 255 / 45%);
    stroke-width: 0.5;
  }
  .trend-casing {
    fill: none;
    stroke: rgb(255 255 255 / 45%);
    stroke-width: 4;
    stroke-dasharray: 3 6;
  }
  .zone-caption {
    flex: 1 1 auto;
    min-width: 0;
    color: var(--accent);
    margin: 0;
  }
  .trend-summary {
    color: var(--ink);
  }
  .zone-footer {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    margin-top: 4px;
  }
  .zone-controls {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 5px;
    color: var(--muted);
    font-size: 11px;
    text-align: right;
  }
  .trace-control,
  .trend-control {
    cursor: pointer;
    white-space: nowrap;
  }
  .trace-control input {
    accent-color: var(--accent);
  }
  .trend-control select {
    max-width: min(230px, 45vw);
    border: 1px solid var(--edge);
    background: var(--panel);
    color: var(--ink);
    font: inherit;
  }
  .trend-control option:disabled {
    color: var(--muted);
  }
  .zone-controls small {
    font-size: 10px;
  }
  @container (max-width: 560px) {
    .zone-footer {
      flex-direction: column;
    }
    .zone-controls {
      align-self: stretch;
      align-items: flex-end;
    }
  }
</style>
