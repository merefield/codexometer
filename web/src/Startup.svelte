<script lang="ts">
  import { onMount } from 'svelte';
  import BlockLogo from './BlockLogo.svelte';
  import {
    createEntrance,
    lettering,
    entranceDuration,
    holdDuration,
    dockDuration,
    logoWidth,
    logoHeight,
  } from './wordmark';

  let { target, onfinish } = $props<{
    target: HTMLAnchorElement | undefined;
    onfinish: () => void;
  }>();
  let entrance = $state<ReturnType<typeof createEntrance>>();
  let elapsed = $state(0);
  let viewport = $state({ width: 0, height: 0, pixelRatio: 1 });
  let destination = $state({ x: 0, y: 0, width: 0 });
  const letters = $derived(entrance ? lettering(entrance, elapsed) : undefined);
  const geometry = $derived.by(() => {
    if (!entrance) return { x: 0, y: 0, width: 0 };
    const duration = entranceDuration[entrance.variant];
    const progress = Math.min(
      Math.max((elapsed - duration - holdDuration) / dockDuration, 0),
      1,
    );
    const eased = progress * progress * (3 - 2 * progress);
    const width = viewport.width + (destination.width - viewport.width) * eased;
    const centre = Math.max(
      (viewport.height - (viewport.width * logoHeight) / logoWidth) / 2,
      0,
    );
    const incoming =
      entrance.variant === 'slide' && elapsed < duration
        ? viewport.width * Math.pow(1 - elapsed / duration, 3)
        : 0;
    const x = incoming + destination.x * eased;
    const y = centre + (destination.y - centre) * eased;
    // Size the SVG viewport itself: compositor scaling can magnify a cached
    // low-resolution raster even when the source artwork is vector. Align its
    // position and dimensions with physical pixels on standard/Retina displays.
    const align = (value: number) =>
      Math.round(value * viewport.pixelRatio) / viewport.pixelRatio;
    return { x: align(x), y: align(y), width: align(width) };
  });

  onMount(() => {
    const motion = matchMedia('(prefers-reduced-motion: reduce)');
    if (motion.matches) {
      onfinish();
      return;
    }
    entrance = createEntrance();
    const duration =
      entranceDuration[entrance.variant] + holdDuration + dockDuration;
    let frame = 0;
    let finished = false;
    const start = performance.now();
    const measure = () => {
      const bounds = target?.getBoundingClientRect();
      viewport = {
        width: window.innerWidth,
        height: window.innerHeight,
        pixelRatio: window.devicePixelRatio || 1,
      };
      if (bounds)
        destination = { x: bounds.x, y: bounds.y, width: bounds.width };
    };
    const finish = () => {
      if (finished) return;
      finished = true;
      cancelAnimationFrame(frame);
      onfinish();
    };
    const skip = (event: KeyboardEvent | PointerEvent) => {
      // Consume the input so it cannot activate a hidden dashboard action.
      if (event instanceof PointerEvent && event.button !== 0) return;
      if (
        event instanceof KeyboardEvent &&
        (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey)
      )
        return;
      if (
        event instanceof KeyboardEvent &&
        ['Shift', 'Control', 'Alt', 'Meta'].includes(event.key)
      )
        return;
      event.preventDefault();
      event.stopImmediatePropagation();
      finish();
    };
    const changedMotion = () => {
      if (motion.matches) finish();
    };
    const animate = (now: number) => {
      elapsed = Math.max(now - start, 0);
      if (elapsed >= duration) finish();
      else frame = requestAnimationFrame(animate);
    };
    // ResizeObserver also catches header wrapping while the first snapshot loads.
    const observer = new ResizeObserver(measure);
    if (target) {
      observer.observe(target);
      if (target.parentElement) observer.observe(target.parentElement);
      const header = target.closest('header');
      if (header) observer.observe(header);
    }
    measure();
    window.addEventListener('resize', measure);
    document.addEventListener('keydown', skip, true);
    document.addEventListener('pointerdown', skip, true);
    motion.addEventListener('change', changedMotion);
    frame = requestAnimationFrame(animate);
    return () => {
      finished = true;
      cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener('resize', measure);
      document.removeEventListener('keydown', skip, true);
      document.removeEventListener('pointerdown', skip, true);
      motion.removeEventListener('change', changedMotion);
    };
  });
</script>

<div
  class="startup"
  role="status"
  aria-label="Starting Codexometer"
  data-entrance={entrance?.variant}
>
  {#if letters}
    <div
      class="wordmark"
      style:left={`${geometry.x}px`}
      style:top={`${geometry.y}px`}
      style:width={`${geometry.width}px`}
      data-text={letters.text}
      data-cursor={letters.dot ? letters.cursor : undefined}
    >
      <BlockLogo
        text={letters.text}
        cursor={letters.cursor}
        dot={letters.dot}
      />
    </div>
  {/if}
  <button type="button" onclick={onfinish}
    >Click or press any key to skip</button
  >
</div>

<style>
  .startup {
    position: fixed;
    inset: 0;
    z-index: 100;
    overflow: hidden;
    background: var(--bg);
    color: var(--accent);
  }
  .wordmark {
    position: absolute;
  }
  button {
    position: absolute;
    bottom: 24px;
    left: 50%;
    transform: translateX(-50%);
    border: 0;
    background: transparent;
    color: var(--muted);
    font: inherit;
    font-size: 12px;
    white-space: nowrap;
  }
</style>
