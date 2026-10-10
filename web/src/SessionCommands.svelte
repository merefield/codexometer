<script lang="ts">
  import { onDestroy, tick } from 'svelte';
  import { controlRequest, live } from './state.svelte';
  import { statusLineUI } from './preferences.svelte';
  let {
    session,
    query = '',
    onchange = () => {},
    oncomplete = (_text: string) => {},
  }: {
    session: string;
    query?: string;
    onchange?: () => void;
    oncomplete?: (text: string) => void;
  } = $props();
  interface Choice {
    id: string;
    label: string;
    help: string;
    next?: string;
    action?: boolean;
    selected?: boolean;
  }
  interface Menu {
    title: string;
    help: string;
    path: string;
    revision: string;
    choices: Choice[];
    input?: boolean;
    picker?: boolean;
    inputLabel?: string;
    inputLimit?: number;
    value?: string;
  }
  let open = $state(false),
    busy = $state(false),
    notice = $state('');
  let menu = $state<Menu | null>(null),
    selected = $state<Choice | null>(null);
  let confirmation = $state(''),
    expires = $state(0),
    now = $state(Date.now());
  let name = $state('');
  let request = 0;
  let selectedIndex = $state(0);
  let dismissedQuery = $state<string | null>(null);
  let optionList: HTMLDivElement | undefined = $state();
  let commandPanel: HTMLDivElement | undefined = $state();
  let reviewPanel: HTMLDivElement | undefined = $state();
  let stagedChoice = $state('');
  const listID = $props.id();
  const picking = $derived(open && !!menu?.picker && !selected);
  const browsing = $derived(open && !menu?.input && !selected);
  const clock = setInterval(() => {
    now = Date.now();
  }, 1000);
  onDestroy(() => {
    clearInterval(clock);
    request++;
  });
  let slash = $derived(
    query.trim().startsWith('/') && !query.trim().startsWith('//'),
  );
  let filter = $derived(
    menu?.path === '' && slash ? query.trim().slice(1).toLowerCase() : '',
  );
  let choices = $derived(
    (menu?.choices || []).filter(
      (o) => !filter || o.label.toLowerCase().startsWith('/' + filter),
    ),
  );
  const stagedSpeed = $derived(
    choices.find((choice) => choice.id === stagedChoice),
  );
  let unavailable = $derived(!live.connected || !!live.data?.sessionsError);
  let suggesting = $derived(
    open && slash && (!menu || menu.path === '') && !selected,
  );
  $effect(() => {
    query;
    selectedIndex = 0;
  });
  $effect(() => {
    const index = selectedIndex;
    choices;
    if (browsing)
      void tick().then(() => {
        const row = optionList?.children[index] as HTMLElement | undefined;
        if (!row || !optionList) return;
        const listRect = optionList.getBoundingClientRect();
        const rowRect = row.getBoundingClientRect();
        if (rowRect.top < listRect.top)
          optionList.scrollTop += rowRect.top - listRect.top;
        else if (rowRect.bottom > listRect.bottom)
          optionList.scrollTop += rowRect.bottom - listRect.bottom;
      });
  });
  function highlight(index: number) {
    if (selectedIndex !== index && menu?.picker) {
      stagedChoice = '';
      confirmation = '';
      expires = 0;
      notice = '';
      request++;
    }
    selectedIndex = index;
  }
  function closeCommands() {
    request++;
    busy = false;
    open = false;
    dismissedQuery = query;
    confirmation = '';
    stagedChoice = '';
    expires = 0;
    oncomplete(query);
  }
  export function handleKey(event: KeyboardEvent) {
    const inside = !!commandPanel?.contains(event.target as Node);
    if (
      !open ||
      menu?.input ||
      (!browsing && !selected) ||
      (!slash && !inside) ||
      unavailable ||
      event.isComposing ||
      event.ctrlKey ||
      event.metaKey ||
      event.altKey
    )
      return;
    const confirmKey = event.key.toLowerCase() === 'c';
    if (confirmKey && !inside) return; // Never consume composer typing.
    if (
      !['ArrowUp', 'ArrowDown', 'Tab', 'Enter', 'Escape'].includes(event.key) &&
      !confirmKey
    )
      return;
    if (
      event.key === 'Tab' &&
      (selected || menu?.path !== '' || event.shiftKey)
    )
      return;
    if (
      event.key === 'Enter' &&
      event.target instanceof Element &&
      event.target.closest('button') &&
      !optionList?.contains(event.target) &&
      !event.target.closest('[data-command-confirm]')
    )
      return;
    if (selected && !inside) return;
    if (selected && ['ArrowUp', 'ArrowDown'].includes(event.key)) return;
    event.preventDefault();
    event.stopPropagation();
    if (event.key === 'Escape') {
      closeCommands();
      return;
    }
    if (busy || (event.repeat && confirmKey)) return;
    if (confirmKey) {
      if (selected?.action || (picking && stagedChoice)) void commit();
      return;
    }
    if (selected) return; // Repeated Enter never confirms a reviewed change.
    if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
      highlight(
        Math.min(
          Math.max(selectedIndex + (event.key === 'ArrowUp' ? -1 : 1), 0),
          Math.max(choices.length - 1, 0),
        ),
      );
      if (!picking && optionList?.contains(event.target as Node))
        void tick().then(() =>
          (
            optionList?.children[selectedIndex] as HTMLElement | undefined
          )?.focus(),
        );
      return;
    }
    const option = choices[selectedIndex];
    if (!option) return;
    if (event.key === 'Tab') oncomplete(option.label);
    else if (picking) void reviewChoice(option);
    else void choose(option);
  }

  $effect(() => {
    if (unavailable) {
      request++;
      busy = false;
      confirmation = '';
      stagedChoice = '';
      if (open) {
        open = false;
        dismissedQuery = query;
      }
      menu = null;
      selected = null;
    }
  });
  $effect(() => {
    if (slash && !open && query !== dismissedQuery && !unavailable) {
      open = true;
      void load('');
    }
  });
  async function load(path: string) {
    const seq = ++request;
    busy = true;
    selected = null;
    stagedChoice = '';
    confirmation = '';
    expires = 0;
    notice = '';
    try {
      const result = await controlRequest<Menu>('commands', {
        session,
        command: { mode: 'list', path },
      });
      if (seq === request) {
        if (path === '' || path === 'help')
          result.choices.push({
            id: 'local-statusline',
            label: '/statusline',
            help: 'Choose multiple fields for Codexometer’s detail footer.',
            next: 'statusline',
          });
        menu = result;
        selectedIndex = result.picker
          ? Math.max(
              result.choices.findIndex((choice) => choice.selected),
              0,
            )
          : 0;
        if (result.picker) void tick().then(() => optionList?.focus());
        name = result.value || '';
        if (
          (result.path.startsWith('rename/') ||
            result.path.startsWith('cd/')) &&
          result.choices.length === 1
        ) {
          busy = false; // The catalogue is ready before preparing its sole choice.
          void choose(result.choices[0]);
        }
      }
    } catch {
      if (seq === request) {
        notice = 'Command catalogue unavailable. Use Codex.';
        if (path === '' || path === 'help')
          menu = {
            title: '/ COMMANDS',
            help: 'Local display settings remain available.',
            path: '',
            revision: '',
            choices: [
              {
                id: 'local-statusline',
                label: '/statusline',
                help: 'Choose multiple fields for Codexometer’s detail footer.',
                next: 'statusline',
              },
            ],
          };
      }
    } finally {
      if (seq === request) busy = false;
    }
  }
  async function choose(o: Choice) {
    if (menu?.picker) {
      highlight(choices.findIndex((choice) => choice.id === o.id));
      optionList?.focus();
      return;
    }
    if (o.next === 'statusline') {
      statusLineUI.open = true;
      open = false;
      dismissedQuery = query;
      return;
    }
    if (o.next) {
      await load(o.next);
      return;
    }
    await reviewChoice(o);
  }
  async function reviewChoice(o: Choice) {
    if (busy || unavailable || !menu) return;
    if (menu.picker) stagedChoice = o.id;
    else {
      selected = o;
      void tick().then(() => reviewPanel?.focus());
    }
    confirmation = '';
    expires = 0;
    notice = '';
    if (!o.action) return;
    const seq = ++request;
    busy = true;
    try {
      const result = await controlRequest<{
        confirmation: string;
        expires: string;
      }>('commands', {
        session,
        command: {
          mode: 'prepare',
          path: menu.path,
          revision: menu.revision,
          choice: o.id,
        },
      });
      if (seq === request) {
        const deadline = Date.parse(result.expires);
        if (
          !result.confirmation ||
          !Number.isFinite(deadline) ||
          Date.now() >= deadline
        )
          throw new Error('Review confirmation unavailable');
        confirmation = result.confirmation;
        expires = deadline;
      }
    } catch {
      if (seq === request)
        notice = 'Option changed or unavailable. Refresh before retrying.';
    } finally {
      if (seq === request) busy = false;
    }
  }

  async function commit() {
    if (
      !confirmation ||
      unavailable ||
      busy ||
      Date.now() >= expires ||
      !menu ||
      (menu.picker && stagedChoice !== choices[selectedIndex]?.id)
    )
      return;
    const token = confirmation;
    confirmation = '';
    busy = true;
    const seq = ++request;
    try {
      const result = await controlRequest<{ message: string }>('commands', {
        session,
        confirmation: token,
        command: { mode: 'commit', path: menu.path },
      });
      if (seq === request) {
        notice = result.message;
        open = false;
        menu = null;
        selected = null;
        stagedChoice = '';
        expires = 0;
        dismissedQuery = null;
        onchange();
      }
    } catch {
      if (seq === request)
        notice = 'Change unconfirmed. Check Codex before retrying.';
    } finally {
      if (seq === request) busy = false;
    }
  }
</script>

{#snippet options()}
  {#each choices as o, index (o.id)}<button
      id={`${listID}-${index}`}
      role={picking ? 'option' : undefined}
      aria-selected={picking ? selectedIndex === index : undefined}
      tabindex={picking ? -1 : undefined}
      disabled={busy || unavailable}
      title={o.help}
      aria-label={o.label +
        (picking && o.selected ? ' // CURRENT' : '') +
        (o.next ? ' →' : !o.action ? ' // HELP' : '')}
      class:suggested={selectedIndex === index}
      onfocus={() => {
        if (!picking) highlight(index);
      }}
      onclick={() => {
        highlight(index);
        void choose(o);
      }}
      ><span class="command-name">{o.label}</span><span class="command-tail"
        >{picking && o.selected ? ' // CURRENT' : ''}{o.next
          ? ' →'
          : !o.action
            ? ' // HELP'
            : ''}</span
      >{#if (slash && menu?.path === '') || picking}<span class="command-help"
          >{o.help}</span
        >{/if}</button
    >{/each}
{/snippet}

<section aria-label="Session slash commands" class="slash-commands">
  <button
    disabled={unavailable || busy}
    onclick={() => {
      open = !open;
      dismissedQuery = open ? null : query;
      confirmation = '';
      if (open) void load('');
    }}
  >
    / COMMANDS
  </button>
  {#if notice && !open}<p role="status">{notice}</p>{/if}
  {#if open}
    <div
      class:command-popup={suggesting}
      role="dialog"
      aria-modal="false"
      tabindex="-1"
      onkeydown={handleKey}
      bind:this={commandPanel}
      aria-label={suggesting
        ? 'Slash command suggestions'
        : 'Session command options'}
    >
      {#if !suggesting}
        <h3>{menu?.title || '/ COMMANDS'}</h3>
        <p class="muted">{menu?.help || 'Loading live options…'}</p>
      {/if}
      {#if notice}<p role="status">{notice}</p>{/if}
      {#if busy}<p role="status">Loading / sending…</p>{/if}
      {#if menu?.input}
        <form
          onsubmit={(event) => {
            event.preventDefault();
            if (menu && name.trim() && !busy && !unavailable)
              void load(menu.path + '/' + encodeURIComponent(name.trim()));
          }}
        >
          <label
            >{(menu.inputLabel || 'Session name').toUpperCase()}
            <input
              aria-label={menu.inputLabel || 'Session name'}
              bind:value={name}
              maxlength={menu.inputLimit || 512}
              disabled={busy || unavailable}
            /></label
          >
          <button type="submit" disabled={busy || unavailable || !name.trim()}
            >REVIEW {menu.path === 'cd' ? 'DIRECTORY' : 'RENAME'}</button
          >
        </form>
      {:else if selected}
        <div
          role="group"
          tabindex="-1"
          aria-label="Review command change"
          bind:this={reviewPanel}
        >
          <h4>{selected.label}</h4>
          <pre>{selected.help || 'No additional help supplied by Codex.'}</pre>
          {#if selected.action}<p class="notice">
              TARGET // {session}.
              {#if menu?.path.startsWith('rename/')}Only this session's saved
                name changes.
              {:else if menu?.path.startsWith('cd/')}Only this session’s
                directory changes; project configuration is not reloaded.
              {:else}No change sent yet. C confirms the selected setting. Model,
                reasoning and speed settings can change token usage and cost.
                Changes affect subsequent turns of this session, not global
                defaults. Automatic quota thresholds may later supersede model
                settings.{/if}
            </p>
            <button
              disabled={busy || unavailable || !confirmation || now >= expires}
              data-command-confirm
              onclick={commit}>C CONFIRM CHANGE</button
            >
            {#if confirmation && now >= expires}<p>
                Confirmation expired. Reopen the option.
              </p>{/if}
          {/if}
          <button
            disabled={busy}
            onclick={() => {
              request++;
              busy = false;
              selected = null;
              confirmation = '';
              void tick().then(() => optionList?.focus());
            }}>BACK</button
          >
        </div>
      {:else}
        {#if picking}
          <div
            class="command-options vertical"
            role="listbox"
            aria-label="Session speed"
            aria-activedescendant={`${listID}-${selectedIndex}`}
            tabindex="0"
            bind:this={optionList}
          >
            {@render options()}
          </div>
        {:else}
          <div
            class="command-options"
            class:vertical={suggesting}
            role="toolbar"
            tabindex="-1"
            aria-label="Command options"
            bind:this={optionList}
          >
            {@render options()}
          </div>
        {/if}
        {#if picking}<p class="muted">{choices[selectedIndex]?.help}</p>
          <p class="muted">
            ↑/↓ select · Enter review · C confirm · Escape cancel
          </p>
          {#if stagedSpeed}<p role="status">
              REVIEW // {stagedSpeed.label}. No change sent yet; C confirms.
            </p>
            {#if confirmation && now >= expires}<p>
                Review expired. Press Enter to review again.
              </p>{/if}
          {/if}
        {/if}
        {#if !busy && !choices.length}<p>
            No available matching commands.
          </p>{/if}
        {#if slash && menu?.path === ''}<p class="muted">
            ↑/↓ select · Tab complete · Enter options · Escape dismiss
          </p>{/if}
      {/if}
      {#if !suggesting}
        {#if picking}<button
            onclick={() => {
              const option = choices[selectedIndex];
              if (option) void reviewChoice(option);
            }}
            disabled={busy ||
              unavailable ||
              !choices[selectedIndex]?.action ||
              !!notice}>ENTER REVIEW</button
          >
          <button
            data-command-confirm
            onclick={commit}
            disabled={busy ||
              unavailable ||
              !confirmation ||
              stagedChoice !== choices[selectedIndex]?.id ||
              now >= expires ||
              !!notice}>C CONFIRM CHANGE</button
          >
        {/if}
        <button disabled={busy} onclick={() => load('')}>ALL COMMANDS</button>
        <button disabled={busy} onclick={() => load(menu?.path || '')}
          >REFRESH OPTIONS</button
        >
        {#if picking}<button
            disabled={busy}
            onclick={() => {
              closeCommands();
            }}>ESC CLOSE</button
          >{/if}
      {/if}
    </div>
  {/if}
</section>

<style>
  .slash-commands {
    margin: 0.4rem 0;
    position: relative;
  }
  .command-popup {
    position: absolute;
    bottom: 100%;
    left: 0;
    z-index: 5;
    width: min(40rem, 100%);
    box-sizing: border-box;
    padding: 0.35rem;
    border: 1px solid var(--accent);
    background: var(--panel);
    box-shadow: 0 0.3rem 1rem #0006;
  }
  .command-popup p {
    margin: 0.3rem 0;
    font-size: 0.8em;
  }
  .command-options.vertical {
    display: flex;
    flex-direction: column;
    flex-wrap: nowrap;
    gap: 0.15rem;
  }
  .vertical button {
    flex: none;
    display: flex;
    align-items: baseline;
    gap: 0.4rem;
    text-align: left;
    width: 100%;
    min-width: 0;
    white-space: nowrap;
    border-color: transparent;
    color: var(--muted);
    background: var(--panel);
  }
  .vertical .command-name,
  .vertical .command-tail {
    flex: none;
  }
  .vertical .command-name {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .vertical button.suggested {
    outline: none;
  }
  .command-options button.suggested .command-name {
    background: var(--accent);
    color: var(--bg);
    font-weight: 700;
  }
  .command-options button:not(.suggested):hover .command-name {
    text-decoration: underline;
  }
  .command-options button {
    color: var(--muted);
  }
  .vertical .command-help {
    flex: 1;
    min-width: 0;
    max-width: none;
    color: var(--muted);
  }
  .command-options {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
    max-height: 35vh;
    overflow: auto;
  }
  .command-options .suggested {
    outline: 1px solid var(--accent);
  }
  .command-help {
    display: block;
    font-size: 0.8em;
    max-width: 32ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    opacity: 0.75;
  }
  pre {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 35vh;
    overflow: auto;
  }
</style>
