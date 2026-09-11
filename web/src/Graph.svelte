<script lang="ts">
  let {
    values = [],
    label = 'Token activity',
    capacity = 0,
  }: { values?: number[]; label?: string; capacity?: number } = $props();
  let peak = $derived(Math.max(1, ...values));
  let plotted = $derived(
    capacity > values.length
      ? [...Array(capacity - values.length).fill(0), ...values]
      : values,
  );
</script>

<p class="eyebrow">
  SCALE // 0 — {Math.max(0, ...values).toLocaleString('en-GB')} TOKENS
</p>
<div
  class="chart"
  role="img"
  aria-label={`${label}. Peak ${peak.toLocaleString('en-GB')} tokens.`}
>
  {#each plotted as value}<div
      class="chart-bar"
      style:height={`${(100 * value) / peak}%`}
      title={value.toLocaleString('en-GB') + ' tokens'}
    ></div>{/each}
</div>
