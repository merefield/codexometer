<script lang="ts">
  let {
    label,
    placeholder,
    field = $bindable(),
    value = $bindable(''),
    disabled = false,
    onkeydown,
  }: {
    label: string;
    placeholder: string;
    field?: HTMLInputElement;
    value?: string;
    disabled?: boolean;
    onkeydown?: (event: KeyboardEvent) => void;
  } = $props();
</script>

<label class="composer-surface session-search"
  ><span aria-hidden="true">⌕</span>
  <input
    type="search"
    aria-label={label}
    {placeholder}
    {disabled}
    maxlength="160"
    bind:this={field}
    bind:value
    {onkeydown}
  />
</label>

<style>
  .session-search {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    max-width: 100%;
  }
  .session-search:focus-within {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  span {
    color: var(--accent);
  }
  input {
    min-width: 0;
    max-width: 100%;
    border: 0;
    outline: 0;
    padding: 0;
    color: inherit;
    background: transparent;
    font: inherit;
  }
</style>
