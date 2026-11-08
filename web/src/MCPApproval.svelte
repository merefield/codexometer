<script lang="ts">
  import type { MCPToolApprovalDetails } from './contracts.generated';
  let { details }: { details: MCPToolApprovalDetails } = $props();
</script>

<section class="mcp-approval" aria-label="MCP tool approval">
  <h3>MCP TOOL APPROVAL</h3>
  <p>{details.message}</p>
  <h3>MCP SERVER</h3>
  <pre>{details.server}</pre>
  {#if details.name}<h3>TOOL</h3>
    <pre>{details.name}</pre>{/if}
  {#if details.title}<h3>TITLE</h3>
    <p>{details.title}</p>{/if}
  {#if details.connector}<h3>APP</h3>
    <p>{details.connector}</p>{/if}
  <h3>ARGUMENTS</h3>
  <pre>{details.arguments || '—'}</pre>
</section>
