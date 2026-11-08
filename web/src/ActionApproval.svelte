<script lang="ts">
  import type { ApprovalReview } from './contracts.generated';
  let { review }: { review: ApprovalReview } = $props();
</script>

<section aria-label={review.title}>
  <h3>{review.title}</h3>
  {#each review.fields || [] as field}
    <h3>{field.label}</h3>
    <pre>{field.value}</pre>
  {/each}
</section>
