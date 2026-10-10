<script lang="ts">
  import { title, logoWidth, logoHeight, wordmarkPath } from './wordmark';
  let {
    text = title,
    cursor = -1,
    dot = false,
  } = $props<{
    text?: string;
    cursor?: number;
    dot?: boolean;
  }>();
  const path = $derived(wordmarkPath(text, cursor, dot));
</script>

<svg
  viewBox={`0 0 ${logoWidth} ${logoHeight}`}
  width={logoWidth}
  height={logoHeight}
  aria-hidden="true"
  focusable="false"
  shape-rendering="crispEdges"><path d={path} /></svg
>

<style>
  svg {
    display: block;
    width: 100%;
    height: auto;
    fill: currentColor;
  }
</style>
