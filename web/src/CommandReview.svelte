<script lang="ts">
  import type { ReviewAssessment } from './contracts.generated';
  let {
    assessment: r,
    full = false,
  }: { assessment: ReviewAssessment; full?: boolean } = $props();
</script>

<section class="assessment" class:attached={r.attached} aria-label={r.title}>
  {#if !r.attached && r.subject}<pre class="command">{r.subject}</pre>{/if}
  <div class="fields">
    <h3>{r.title}</h3>
    <span class="verdict"
      ><span class="dot" data-status={r.status} aria-hidden="true">●</span>
      {r.decision}</span
    >
    {#if r.risk}<span>RISK: {r.risk.toUpperCase()}</span>{/if}
    {#if full && r.authorization}<span
        >USER AUTHORISATION: {r.authorization.toUpperCase()}</span
      >{/if}
  </div>
  {#if r.rationale}<p>{r.rationale}</p>{/if}
</section>

<style>
  .assessment {
    --review-gap: clamp(0.25em, calc(8dvh - 3rem), 1lh);
    margin-block: 0.35em;
    color: color-mix(in srgb, var(--muted) 75%, var(--ink));
  }
  .assessment.attached {
    margin-block-start: var(--review-gap);
  }
  .assessment > .command {
    color: var(--ink);
    margin-block: 0 var(--review-gap);
  }
  .fields {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0.15em 1.1em;
  }
  .fields > * {
    margin: 0;
    max-width: 100%;
    overflow-wrap: anywhere;
  }
  .fields h3 {
    color: inherit;
    font-weight: 400;
  }
  .verdict {
    color: color-mix(in srgb, var(--ink) 70%, var(--muted));
  }
  .dot {
    color: var(--accent);
  }
  .dot[data-status='approved'] {
    color: #89dba3;
  }
  .dot[data-status='denied'] {
    color: #ff9292;
  }
  .dot[data-status='timedOut'],
  .dot[data-status='aborted'] {
    color: var(--approval);
  }
  p {
    margin: 0.2em 0 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
