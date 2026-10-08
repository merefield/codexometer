<script lang="ts">
  import { live, type Session } from './state.svelte';
  import {
    preferences,
    savePreferences,
    statusLineUI,
  } from './preferences.svelte';
  let { session, stale = false }: { session: Session; stale?: boolean } =
    $props();
  let draft = $state<string[]>([]),
    opened = $state(false);
  let fields = $derived(live.data?.statusLineFields || []);
  const normalize = (ids: string[] | null) =>
    [
      ...new Set(ids ?? fields.filter((f) => f.default).map((f) => f.id)),
    ].filter((id) => fields.some((f) => f.id === id));
  const text = (ids: string[] | null) =>
    normalize(ids)
      .map((id) =>
        id === 'run-state' &&
        (stale || !live.connected || live.data?.sessionsError)
          ? 'STALE'
          : session.statusLine?.[id],
      )
      .filter(Boolean)
      .join(' · ');
  let entries = $derived([
    ...draft.map((id) => fields.find((f) => f.id === id)!).filter(Boolean),
    ...fields.filter((f) => !draft.includes(f.id)),
  ]);
  $effect(() => {
    if (statusLineUI.open && !opened) {
      draft = normalize(preferences.statusLine);
      opened = true;
    } else if (!statusLineUI.open) opened = false;
  });
  function toggle(id: string, checked: boolean) {
    draft = checked ? [...draft, id] : draft.filter((value) => value !== id);
  }
  function move(id: string, delta: number) {
    const at = draft.indexOf(id),
      to = at + delta;
    if (at < 0 || to < 0 || to >= draft.length) return;
    const next = [...draft];
    [next[at], next[to]] = [next[to], next[at]];
    draft = next;
  }
</script>

{#if statusLineUI.open}
  <section class="status-picker" aria-label="Status line fields">
    <h3>STATUS LINE // CODEXOMETER</h3>
    <p class="muted">
      Choose multiple fields and their order. These display preferences do not
      change Codex CLI configuration. Missing values are omitted.
    </p>
    <div class="fields">
      {#each entries as field (field.id)}
        <div class="field">
          <label title={field.help}
            ><input
              type="checkbox"
              checked={draft.includes(field.id)}
              onchange={(event) =>
                toggle(field.id, event.currentTarget.checked)}
            />{field.label}<span class="muted">{field.help}</span></label
          >
          {#if draft.includes(field.id)}
            <button
              aria-label={'Move ' + field.label + ' earlier'}
              disabled={draft.indexOf(field.id) === 0}
              onclick={() => move(field.id, -1)}>↑</button
            >
            <button
              aria-label={'Move ' + field.label + ' later'}
              disabled={draft.indexOf(field.id) === draft.length - 1}
              onclick={() => move(field.id, 1)}>↓</button
            >
          {/if}
        </div>
      {/each}
    </div>
    <p class="muted" aria-label="Status line preview">
      Preview // {text(draft) || '—'}
    </p>
    <button
      onclick={() => {
        preferences.statusLine = [...draft];
        savePreferences();
        statusLineUI.open = false;
      }}>APPLY</button
    >
    <button
      onclick={() => {
        statusLineUI.open = false;
      }}>CANCEL</button
    >
  </section>
{/if}
<div class="status-line">
  <p
    class="muted"
    aria-label="Session status line"
    title={text(preferences.statusLine)}
  >
    {text(preferences.statusLine)}
  </p>
  <button
    class="configure"
    onclick={() => {
      statusLineUI.open = !statusLineUI.open;
    }}
    aria-label="Configure status line">/statusline</button
  >
</div>

<style>
  .status-line {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    min-width: 0;
    margin-top: 0.5rem;
  }
  .status-line p {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    margin: 0;
    font-size: 0.8rem;
  }
  .configure {
    font-size: 0.75rem;
    padding: 0.15rem 0.35rem;
  }
  .status-picker {
    border: 1px solid var(--edge);
    padding: 0.6rem;
    margin: 0.5rem 0;
  }
  .fields {
    max-height: 35vh;
    overflow: auto;
  }
  .field {
    display: flex;
    align-items: center;
    gap: 0.35rem;
  }
  .field label {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }
  .field label span {
    font-size: 0.75rem;
  }
  .field button {
    padding: 0.1rem 0.35rem;
  }
</style>
