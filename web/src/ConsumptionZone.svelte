<script lang="ts">
  let { used, elapsed }: { used: number; elapsed: number } = $props();
  const gradient = $props.id();
  const ticks = [0, 25, 50, 75, 100];
  let x = $derived(50 + Math.max(0, Math.min(100, elapsed)) * 3.2);
  let y = $derived(260 - Math.max(0, Math.min(100, used)) * 2.4);
  let difference = $derived(used - elapsed);
</script>

<svg
  class="consumption-zone"
  viewBox="0 0 400 320"
  role="img"
  aria-label={`Consumption zone: ${used}% consumed, ${elapsed.toFixed(1)}% of quota period elapsed. ${difference > 0 ? 'Above' : difference < 0 ? 'Below' : 'On'} the steady-consumption line.`}
>
  <defs>
    <linearGradient id={gradient} x1="0%" y1="0%" x2="100%" y2="100%">
      <stop offset="0%" stop-color="#b93749" />
      <stop offset="50%" stop-color="#8c793d" />
      <stop offset="100%" stop-color="#23855d" />
    </linearGradient>
  </defs>
  <rect x="50" y="20" width="320" height="240" fill={`url(#${gradient})`} />
  {#each ticks as tick}
    <line
      class="grid"
      x1={50 + tick * 3.2}
      y1="20"
      x2={50 + tick * 3.2}
      y2="260"
    />
    <line
      class="grid"
      x1="50"
      y1={260 - tick * 2.4}
      x2="370"
      y2={260 - tick * 2.4}
    />
    <text x={50 + tick * 3.2} y="281" text-anchor="middle">{tick}%</text>
    <text x="36" y={264 - tick * 2.4} text-anchor="end">{tick}%</text>
  {/each}
  <path class="axes" d="M50 20 V260 H370" />
  <line class="pace-line" x1="50" y1="260" x2="370" y2="20" />
  <text x="50" y="12" class="axis-title">CONSUMPTION</text>
  <text x="210" y="308" text-anchor="middle" class="axis-title"
    >QUOTA PERIOD ELAPSED</text
  >
  <circle class="position-halo" cx={x} cy={y} r="10" />
  <circle class="position-dot" cx={x} cy={y} r="5">
    <title>{used}% consumed // {elapsed.toFixed(1)}% of period elapsed</title>
  </circle>
</svg>
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

<style>
  svg {
    display: block;
    width: 100%;
    margin: 20px auto 0;
    max-height: 65vh;
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
  .zone-caption {
    color: var(--accent);
    text-align: center;
  }
</style>
