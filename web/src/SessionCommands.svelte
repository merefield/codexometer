<script lang="ts">
  import { onDestroy, tick } from 'svelte';
  import { controlRequest, live } from './state.svelte';
  import { statusLineUI, themeUI } from './preferences.svelte';
  import { sessionCopyText } from './session-copy';
  let {
    session,
    query = '',
    localOnly = false,
    oncopy = () => {},
    onchange = () => {},
    oncomplete = (_text: string) => {},
  }: {
    session: string;
    query?: string;
    localOnly?: boolean;
    oncopy?: () => void | Promise<void>;
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
    unavailable?: string;
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
  const localPaths = ['statusline', 'usage', 'copy', 'theme', 'exit', 'quit'];
  function localChoices(): Choice[] {
    return [
      {
        id: 'local-statusline',
        label: '/statusline',
        help: 'Choose fields for Codexometer’s detail footer.',
        next: 'statusline',
      },
      {
        id: 'local-usage',
        label: '/usage',
        help: 'Open Codexometer’s usage history. Quota and reset views remain available.',
        next: 'usage',
      },
      {
        id: 'local-copy',
        label: '/copy',
        help: 'Copy the selected session’s observed reply or activity with its Copy control.',
        next: 'copy',
      },
      {
        id: 'local-theme',
        label: '/theme',
        help: 'Open the existing Codexometer theme selector. Saved browser display preferences only.',
        next: 'theme',
      },
      {
        id: 'local-exit',
        label: '/exit',
        help: 'Leave session detail and return to Sessions. The local server remains running.',
        next: 'exit',
      },
      {
        id: 'local-quit',
        label: '/quit',
        help: 'Alias for /exit: return to Sessions. The local server remains running.',
        next: 'quit',
      },
    ];
  }
  function localMenu(): Menu {
    return {
      title: '/ COMMANDS',
      help: 'Local Codexometer commands remain available without session control.',
      path: '',
      revision: '',
      choices: localChoices(),
    };
  }
  function finishLocal() {
    request++;
    busy = false;
    open = false;
    dismissedQuery = query;
    confirmation = '';
    stagedChoice = '';
    expires = 0;
    history = [];
    notice = '';
    if (slash) onchange();
    else oncomplete(query);
  }
  async function runLocal(path: string): Promise<boolean> {
    if (!localPaths.includes(path)) return false;
    if (
      path === 'copy' &&
      !sessionCopyText(live.data?.sessions.find((row) => row.id === session))
    ) {
      notice =
        'No reply or activity text is available to copy for this session.';
      return true;
    }
    if (path === 'copy') {
      const operation = oncopy();
      finishLocal();
      await operation;
    } else {
      finishLocal();
      if (path === 'usage') location.hash = '/usage';
      else if (path === 'theme') themeUI.requests++;
      else if (path === 'statusline') statusLineUI.open = true;
      else location.hash = '/sessions';
    }
    return true;
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
  interface Level {
    path: string;
    index: number;
    input: string;
  }
  let history = $state<Level[]>([]);
  const inputReview = $derived(
    menu?.path.startsWith('rename/') || menu?.path.startsWith('cd/'),
  );
  const canBack = $derived(
    (!!selected && !inputReview) || !!stagedChoice || history.length > 0,
  );
  function availability(o: Choice) {
    if (o.unavailable)
      return o.unavailable.startsWith('Waiting for idle.')
        ? ' // WAITING FOR IDLE'
        : ' // UNAVAILABLE';
    return o.next ? ' →' : !o.action ? ' // HELP' : '';
  }
  async function navigate(path: string) {
    // A filtered slash suggestion opens the command directly, with no parent menu.
    if (menu && !(suggesting && filter))
      history = [
        ...history,
        { path: menu.path, index: selectedIndex, input: name },
      ];
    await load(path);
  }
  async function back() {
    if (!canBack) return;
    request++;
    busy = false;
    confirmation = '';
    expires = 0;
    notice = '';
    if (stagedChoice) {
      stagedChoice = '';
      optionList?.focus();
      return;
    }
    if (selected && !inputReview) {
      selected = null;
      await tick();
      optionList?.focus();
      return;
    }
    const level = history.at(-1);
    if (!level) return;
    history = history.slice(0, -1);
    await load(level.path, level);
  }
  function reviewInput() {
    if (menu && name.trim() && !busy && !unavailable)
      void navigate(menu.path + '/' + encodeURIComponent(name.trim()));
  }
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
  const currentChoice = $derived(selected || choices[selectedIndex]);
  const reviewed = $derived(
    !!confirmation &&
      now < expires &&
      (menu?.picker ? stagedChoice === currentChoice?.id : !!selected),
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
    history = [];
    oncomplete(query);
  }
  export function handleKey(event: KeyboardEvent) {
    const inside = !!commandPanel?.contains(event.target as Node);
    if (
      !open ||
      (!browsing && !selected && !menu?.input) ||
      (!slash && !inside) ||
      (unavailable && !localOnly) ||
      event.isComposing ||
      event.ctrlKey ||
      event.metaKey ||
      event.altKey
    )
      return;
    if (menu?.input) {
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        closeCommands();
      }
      return; // Editing keys belong to the input.
    }
    const backKey = event.key === 'ArrowLeft';
    const refreshKey = event.key.toLowerCase() === 'f';
    const confirmKey = event.key.toLowerCase() === 'c';
    if ((confirmKey || refreshKey || backKey) && !inside) return; // Never consume composer typing.
    if (
      !['ArrowUp', 'ArrowDown', 'Tab', 'Enter', 'Escape'].includes(event.key) &&
      !confirmKey &&
      !refreshKey &&
      !backKey
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
    if (backKey) {
      if (canBack) void back();
      return;
    }
    if (refreshKey) {
      void load(menu?.path || '');
      return;
    }
    if (confirmKey) {
      if (selected?.action || (picking && stagedChoice)) void commit();
      return;
    }
    if (selected) {
      if (event.key === 'Enter' && selected.action && !reviewed && !notice)
        void reviewChoice(selected);
      return; // Repeated Enter never confirms a reviewed change.
    }
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
    if (unavailable && !localOnly) {
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
    if (
      slash &&
      !open &&
      query !== dismissedQuery &&
      (!unavailable || localOnly)
    ) {
      open = true;
      void load('');
    }
  });
  async function load(path: string, restore?: Level) {
    const seq = ++request;
    if (menu) menu = { ...menu, path };
    if (restore) name = restore.input;
    busy = true;
    selected = null;
    stagedChoice = '';
    confirmation = '';
    expires = 0;
    notice = '';
    if (localOnly) {
      menu = localMenu();
      selectedIndex = restore?.index ?? 0;
      busy = false;
      return;
    }
    try {
      const result = await controlRequest<Menu>('commands', {
        session,
        command: { mode: 'list', path },
      });
      if (seq === request) {
        if (path === '' || path === 'help')
          result.choices = [
            ...result.choices.filter(
              (o) => !localPaths.includes(o.label.replace(/^\//, '')),
            ),
            ...localChoices(),
          ];
        menu = result;
        selectedIndex = restore
          ? Math.min(restore.index, Math.max(result.choices.length - 1, 0))
          : result.picker
            ? Math.max(
                result.choices.findIndex((choice) => choice.selected),
                0,
              )
            : 0;
        if (result.picker) void tick().then(() => optionList?.focus());
        name = restore?.input ?? result.value ?? '';
        if (restore)
          void tick().then(() => {
            if (result.input) commandPanel?.querySelector('input')?.focus();
            else optionList?.focus();
          });
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
        if (path === '' || path === 'help') {
          menu = localMenu();
          notice = '';
        } else
          menu = {
            title: '/' + path,
            help: '',
            path,
            revision: '',
            choices: [],
          };
      }
    } finally {
      if (seq === request) busy = false;
    }
  }
  async function choose(o: Choice) {
    if (o.unavailable || busy) return;
    if (o.next && (await runLocal(o.next))) return;
    if (notice) return;
    if (menu?.picker) {
      highlight(choices.findIndex((choice) => choice.id === o.id));
      optionList?.focus();
      return;
    }
    if (o.next) {
      await navigate(o.next);
      return;
    }
    await reviewChoice(o);
  }
  async function reviewChoice(o: Choice) {
    if (busy || unavailable || !menu || o.unavailable || notice) return;
    if (menu.picker) {
      if (reviewed && stagedChoice === o.id) return;
      stagedChoice = o.id;
    } else {
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
      disabled={busy || (unavailable && !localOnly)}
      title={o.help}
      aria-label={o.label +
        (picking && o.selected ? ' // CURRENT' : '') +
        availability(o)}
      class:suggested={selectedIndex === index}
      onfocus={() => {
        if (!picking) highlight(index);
      }}
      onclick={() => {
        highlight(index);
        void choose(o);
      }}
      ><span class="command-name">{o.label}</span><span class="command-tail"
        >{picking && o.selected ? ' // CURRENT' : ''}{availability(o)}</span
      >{#if menu?.path === '' || picking}<span class="command-help"
          >{o.help}</span
        >{/if}</button
    >{/each}
{/snippet}

<section aria-label="Session slash commands" class="slash-commands">
  <button
    disabled={(unavailable && !localOnly) || busy}
    onclick={() => {
      open = !open;
      dismissedQuery = open ? null : query;
      confirmation = '';
      if (open) {
        history = [];
        void load('');
      } else closeCommands();
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
          id={`${listID}-input`}
          onsubmit={(event) => {
            event.preventDefault();
            reviewInput();
          }}
        >
          <label
            >{(menu.inputLabel || 'Session name').toUpperCase()}
            <input
              aria-label={menu.inputLabel || 'Session name'}
              bind:value={name}
              maxlength={menu.inputLimit || 512}
              disabled={busy || (unavailable && !localOnly)}
            /></label
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
              {:else}No change sent yet. Model, reasoning and speed settings can
                change token usage and cost. Changes affect subsequent turns of
                this session, not global defaults. Automatic quota thresholds
                may later supersede model settings.{/if}
            </p>
          {/if}
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

          {#if stagedSpeed}<p role="status">
              REVIEW // {stagedSpeed.label}. No change sent yet.
            </p>
            {#if confirmation && now >= expires}<p>
                Review expired. Press Enter to review again.
              </p>{/if}
          {/if}
        {/if}
        {#if !busy && !choices.length}<p>
            No available matching commands.
          </p>{/if}
      {/if}
      {#if currentChoice?.unavailable}<p role="status" class="availability">
          {currentChoice.unavailable}
        </p>{/if}
      {#if confirmation && now >= expires && !picking}<p role="status">
          Review expired. Review the option again.
        </p>{/if}
      <div class="command-actions" role="toolbar" aria-label="Command actions">
        {#if !notice}
          {#if menu?.input}<button
              type="submit"
              form={`${listID}-input`}
              disabled={busy || unavailable || !name.trim()}
              >ENTER REVIEW</button
            >
          {:else if currentChoice?.action && !currentChoice.unavailable}
            {#if reviewed}<button
                data-command-confirm
                onclick={commit}
                disabled={busy || (unavailable && !localOnly)}
                >C CONFIRM CHANGE</button
              >
            {:else}<button
                onclick={() => {
                  if (currentChoice) void reviewChoice(currentChoice);
                }}
                disabled={busy || (unavailable && !localOnly)}
                >ENTER REVIEW</button
              >{/if}
          {:else if !selected && currentChoice && !currentChoice.unavailable}<button
              onclick={() => {
                if (currentChoice) void choose(currentChoice);
              }}
              disabled={busy || (unavailable && !localOnly)}
              >ENTER {currentChoice.next ? 'OPEN' : 'HELP'}</button
            >{/if}
        {/if}
        {#if canBack}<button disabled={busy} onclick={back}>← BACK</button>{/if}
        {#if !suggesting}<button
            disabled={busy}
            onclick={() => load(menu?.path || '')}>F REFRESH OPTIONS</button
          >{/if}
        <button onclick={closeCommands}>ESC CANCEL</button>
      </div>
      {#if !menu?.input && !selected}<p class="navigation">
          ↑/↓ select{suggesting ? ' · Tab complete' : ''}
        </p>{/if}
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
    color: var(--ink);
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
    outline: none;
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
  .command-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
    margin-top: 0.4rem;
  }
  .availability {
    font-weight: 600;
  }
  .navigation {
    font-size: 0.8em;
  }
  pre {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 35vh;
    overflow: auto;
  }
</style>
