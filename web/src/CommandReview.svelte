<script lang="ts">
  import type { ReviewAssessment } from './contracts.generated';
  let {
    assessment: r,
    full = false,
  }: { assessment: ReviewAssessment; full?: boolean } = $props();
</script>

<section class="assessment" aria-label={r.title}>
  {#if !r.attached && r.subject}<pre class="command">{r.subject}</pre>{/if}
  <div class="fields">
    <h3>{r.title}</h3>
    <strong data-status={r.status}>● {r.decision}</strong>
    {#if r.risk}<span>RISK: {r.risk.toUpperCase()}</span>{/if}
    {#if full && r.authorization}<span
        >USER AUTHORISATION: {r.authorization.toUpperCase()}</span
      >{/if}
  </div>
  {#if r.rationale}<p>{r.rationale}</p>{/if}
</section>

<style>
  .assessment {
    margin-block: 0.35em;
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
  .fields span {
    color: var(--muted);
  }
  strong {
    color: var(--accent);
  }
  strong[data-status='approved'] {
    color: #89dba3;
  }
  strong[data-status='denied'] {
    color: #ff9292;
  }
  strong[data-status='timedOut'],
  strong[data-status='aborted'] {
    color: var(--approval);
  }
  p {
    margin: 0.2em 0 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
