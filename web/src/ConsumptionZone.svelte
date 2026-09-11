<script lang="ts">
  import type { Meter } from './state.svelte';
  import { date } from './state.svelte';
  let {
    used,
    elapsed,
    trail = [],
  }: { used: number; elapsed: number; trail?: Meter['trail'] } = $props();
  const gradient = $props.id();
  let showObservations = $state(false);
  const ticks = [0, 25, 50, 75, 100];
  let width = $state(400);
  let height = $state(240);
  let right = $derived(width - 24);
  let bottom = $derived(height - 48);
  let plotWidth = $derived(Math.max(1, right - 48));
  let plotHeight = $derived(Math.max(1, bottom - 20));
  let x = $derived(
    48 + (Math.max(0, Math.min(100, elapsed)) / 100) * plotWidth,
  );
  let y = $derived(
    bottom - (Math.max(0, Math.min(100, used)) / 100) * plotHeight,
  );
  let difference = $derived(used - elapsed);
  let path = $derived(
    trail
      .map(
        (p, i) =>
          `${i === 0 || p.break ? 'M' : 'L'}${48 + (p.elapsed / 100) * plotWidth} ${bottom - (p.used / 100) * plotHeight}`,
      )
      .join(' '),
  );
</script>

<div class="zone-canvas" bind:clientWidth={width} bind:clientHeight={height}>
  <svg
    class="consumption-zone"
    viewBox={`0 0 ${width} ${height}`}
    role="img"
    aria-describedby={trail.length ? gradient + '-trail-summary' : undefined}
    aria-label={`Consumption zone: ${used}% consumed, ${elapsed.toFixed(1)}% of quota period elapsed. ${difference > 0 ? 'Above' : difference < 0 ? 'Below' : 'On'} the steady-consumption line.`}
  >
    <defs>
      <linearGradient id={gradient} x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" stop-color="#b93749" />
        <stop offset="50%" stop-color="#8c793d" />
        <stop offset="100%" stop-color="#23855d" />
      </linearGradient>
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
      <line
        class="grid"
        x1="48"
        y1={bottom - (tick / 100) * plotHeight}
        x2={right}
        y2={bottom - (tick / 100) * plotHeight}
      />
      <text
        x={48 + (tick / 100) * plotWidth}
        y={bottom + 20}
        text-anchor="middle">{tick}%</text
      >
      <text x="34" y={bottom + 4 - (tick / 100) * plotHeight} text-anchor="end"
        >{tick}%</text
      >
    {/each}
    <path class="axes" d={`M48 20 V${bottom} H${right}`} />
    <line class="pace-line" x1="48" y1={bottom} x2={right} y2="20" />
    {#if trail.length}
      <path class="observation-trail" d={path} />
      <circle
        class="trail-start"
        cx={48 + (trail[0].elapsed / 100) * plotWidth}
        cy={bottom - (trail[0].used / 100) * plotHeight}
        r="5"><title>First observation: {date(trail[0].at)}</title></circle
      >
    {/if}
    <text x="48" y="12" class="axis-title">CONSUMPTION</text>
    <text
      x={48 + plotWidth / 2}
      y={height - 5}
      text-anchor="middle"
      class="axis-title">QUOTA PERIOD ELAPSED</text
    >
    <circle class="position-halo" cx={x} cy={y} r="10" />
    <circle class="position-dot" cx={x} cy={y} r="5">
      <title>{used}% consumed // {elapsed.toFixed(1)}% of period elapsed</title>
    </circle>
  </svg>
</div>
<p class="zone-caption">
  {used}% USED // {elapsed.toFixed(1)}% TIME ELAPSED<br />
  <span class="muted"
    >{Math.abs(difference) < 0.05
      ? 'ON PACE'
      : difference > 0
        ? 'ABOVE THE LINE — CONSUMING FASTER THAN TIME'
        : 'BELOW THE LINE — WITHIN PACE'}</span
  >
</p>
{#if trail.length}<p
    class="muted trail-caption"
    id={gradient + '-trail-summary'}
  >
    ○ START {date(trail[0].at)} // {trail.length}
    {trail.length === 1 ? 'OBSERVATION' : 'OBSERVATIONS'}<br />Observed quota
    path, not individual session usage. Gaps are not interpolated. Expand the
    observation table for times, positions and gaps.
  </p>
  <details class="observation-details" bind:open={showObservations}>
    <summary>OBSERVATION TABLE</summary>
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
    stroke: white;
    stroke-width: 2.5;
    stroke-linejoin: round;
  }
  .trail-start {
    fill: none;
    stroke: white;
    stroke-width: 2;
  }
  .trail-caption {
    font-size: 11px;
    text-align: center;
  }
  .zone-caption {
    color: var(--accent);
    text-align: center;
  }
</style>
