import { test as base, expect } from '@playwright/test';
import { spawn } from 'node:child_process';
import { resolve } from 'node:path';
import { readFileSync } from 'node:fs';
import type { Page } from '@playwright/test';

// Keep the stream open so tests can distinguish fresh signals from disconnected
// cached snapshots and can deliver updates without reloading the presentation.
async function mockStream(page: Page, snapshot: object) {
  await page.addInitScript((snapshot) => {
    const original = window.fetch.bind(window);
    window.fetch = (input, init) => {
      if (input === '/api/events') {
        document.documentElement.dataset.testStreams = String(
          Number(document.documentElement.dataset.testStreams || 0) + 1,
        );
        return Promise.resolve(
          new Response(
            new ReadableStream({
              start(controller) {
                const send = (value: unknown) =>
                  controller.enqueue(
                    new TextEncoder().encode(
                      'data: ' + JSON.stringify(value) + '\n\n',
                    ),
                  );
                send(snapshot);
                window.addEventListener(
                  'test-disconnect',
                  () => controller.error(new Error('Test stream closed')),
                  { once: true },
                );
                window.addEventListener('test-snapshot', (event) =>
                  send((event as CustomEvent).detail),
                );
              },
            }),
            { headers: { 'Content-Type': 'text/event-stream' } },
          ),
        );
      }
      return original(input, init);
    };
  }, snapshot);
}

const test = base.extend<{
  pairingURL: string;
  controlMode: boolean;
  quotaMode: string;
}>({
  controlMode: [false, { option: true }],
  quotaMode: ['', { option: true }],
  pairingURL: async ({ controlMode, quotaMode }, use) => {
    const child = spawn(
      process.env.CODEXOMETER_TEST_BINARY ||
        resolve(
          '..',
          process.platform === 'win32' ? 'codexometer.exe' : 'codexometer',
        ),
      [
        '--web',
        '--demo',
        ...(controlMode ? ['--web-control'] : []),
        ...(quotaMode
          ? ['--quota-step-down', '1:gpt-5.6-luna:medium::' + quotaMode]
          : []),
      ],
      { stdio: ['ignore', 'pipe', 'pipe'] },
    );
    let output = '';
    try {
      const url = await new Promise<string>((resolveURL, reject) => {
        const timer = setTimeout(
          () => reject(new Error('Web server did not start')),
          10_000,
        );
        child.once('error', (error) => {
          clearTimeout(timer);
          reject(error);
        });
        child.once('exit', (code) => {
          clearTimeout(timer);
          reject(new Error(`Web server exited: ${code}`));
        });
        child.stdout.on('data', (data) => {
          output += data.toString();
          const match = output.match(
            /http:\/\/127\.0\.0\.1:\d+\/#pair=[A-Z2-7]+/,
          );
          if (match) {
            clearTimeout(timer);
            resolveURL(match[0]);
          }
        });
      });
      await use(url);
    } finally {
      if (child.exitCode === null) {
        const exited = new Promise<void>((resolveExit) =>
          child.once('exit', () => resolveExit()),
        );
        child.kill('SIGINT');
        await exited;
      }
    }
  },
});

// Existing dashboard tests open directly; startup motion is exercised separately.
test.use({ reducedMotion: 'reduce' });

test.describe('quota profile reviews', () => {
  test.use({ controlMode: true, quotaMode: 'ask' });
  test('profile and native approval coexist and confirm independently', async ({
    page,
    pairingURL,
  }) => {
    await page.goto(pairingURL);
    const thresholds = page.getByRole('link', {
      name: 'THRESHOLDS',
      exact: true,
    });
    await expect(thresholds).toBeVisible();
    await expect(
      page
        .getByRole('navigation', { name: 'Main navigation' })
        .getByRole('link', { name: 'THRESHOLDS' }),
    ).toHaveCount(0);
    await expect(
      page
        .getByRole('navigation', { name: 'Quota view' })
        .getByRole('link')
        .last(),
    ).toHaveText('THRESHOLDS');
    await thresholds.click();
    await expect(
      page.getByRole('heading', { name: /THRESHOLDS/ }),
    ).toBeVisible();
    await expect(page.locator('.threshold-list')).toContainText('gpt-5.6-luna');
    await expect(page.locator('.threshold-list')).toContainText('ASK');
    await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
    const pill = page.getByRole('link', { name: /QUOTA THRESHOLD/ }).first();
    await expect(pill).toBeVisible({ timeout: 15000 });
    await pill.click();
    const profile = page.locator('.profile-review').first();
    await expect(profile).toContainText('CURRENT PROFILE');
    await expect(profile).toContainText('PROPOSED PROFILE');
    // Each pill opens only its own review; native approval stays reachable.
    await expect(
      page.getByRole('radio', { name: 'APPROVE ONCE', exact: true }),
    ).toBeHidden();
    await expect(page.locator('.detail-context')).toBeHidden();
    await expect(page.getByRole('button', { name: 'Copy text' })).toHaveCount(
      0,
    );
    await page
      .getByRole('navigation', { name: 'Sessions needing attention' })
      .getByRole('link', { name: /APPROVAL NEEDED/ })
      .first()
      .click();
    await expect(
      page.getByRole('radio', { name: 'APPROVE ONCE', exact: true }),
    ).toBeVisible();
    await expect(profile.getByRole('radio')).toHaveCount(0);
    await page
      .getByRole('radio', { name: 'APPROVE ONCE', exact: true })
      .check();
    await page.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
    await expect(
      page.getByRole('button', { name: 'CONFIRM APPROVE ONCE' }),
    ).toBeVisible();
    await pill.click();
    await expect(profile).toContainText('CURRENT PROFILE');
    await page
      .getByRole('navigation', { name: 'Sessions needing attention' })
      .getByRole('link', { name: /APPROVAL NEEDED/ })
      .first()
      .click();
    await expect(
      page.getByRole('button', { name: 'CONFIRM APPROVE ONCE' }),
    ).toHaveCount(0);
    await pill.click();
    // The review target survives reload/deep links.
    await page.reload();
    await expect(profile).toContainText('CURRENT PROFILE');
    await expect(
      page.getByRole('radio', { name: 'APPROVE ONCE', exact: true }),
    ).toBeHidden();
    await profile
      .getByRole('radio', { name: 'APPLY PROFILE', exact: true })
      .check();
    await profile
      .getByRole('button', { name: 'REVIEW BEFORE SENDING' })
      .click();
    await expect(
      profile.getByRole('button', { name: 'CONFIRM APPLY PROFILE' }),
    ).toBeVisible();
    await profile
      .getByRole('button', { name: 'CONFIRM APPLY PROFILE' })
      .click();
    await expect(profile).toHaveCount(0, { timeout: 15000 });
    await expect(
      page.getByRole('radio', { name: 'APPROVE ONCE', exact: true }),
    ).toBeVisible();
  });
});

test('live detail separates the task and guidance from streamed replies', async ({
  page,
  pairingURL,
}) => {
  const session = {
    id: 'stream-root',
    name: 'Stream test',
    directory: '/work',
    tokens: 1,
    agents: 0,
    status: 'WORKING',
    contextKind: 'LAST REPLY',
    text: 'Partial reply',
    command: '',
    source: 'LIVE',
    activity: '',
    samples: [],
    currentTask: 'Build the <widget>',
    latestGuidance: 'Use Go',
    streaming: true,
  };
  const snapshot = {
    control: false,
    sessionsAt: new Date().toISOString(),
    meters: [],
    credits: [],
    sessions: [session],
  };
  await mockStream(page, snapshot);
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  await page.getByRole('button', { name: 'SHOW ALL DETAILS' }).click();
  await expect(
    page.getByRole('heading', { name: 'CURRENT TASK', exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText('Build the <widget>', { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole('heading', { name: 'LATEST GUIDANCE', exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole('heading', { name: 'REPLY // STREAMING', exact: true }),
  ).toBeVisible();
  session.text = 'Authoritative finished reply';
  session.streaming = false;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(
    page.getByRole('heading', { name: 'LAST REPLY', exact: true }),
  ).toBeVisible();
  await expect(page.getByText(session.text, { exact: true })).toBeVisible();
  await expect(
    page.getByRole('heading', { name: 'TASK', exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(session.currentTask, { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole('heading', { name: 'CURRENT TASK', exact: true }),
  ).toHaveCount(0);
});

test('working detail preserves prose alongside command state', async ({
  page,
  pairingURL,
}) => {
  const session = {
    id: 'working-root',
    name: 'Build check',
    directory: '/work',
    tokens: 1,
    agents: 0,
    status: 'WORKING',
    contextKind: 'LAST ACTIVITY',
    text: 'Checking the build <not markup>',
    command: '',
    source: 'LIVE',
    activity: '',
    samples: [],
    workingCommand: 'go test ./...',
    commandStatus: 'running',
    runningCommands: 2,
  };
  const snapshot = {
    control: false,
    sessionsAt: new Date().toISOString(),
    meters: [],
    credits: [],
    sessions: [session],
  };
  await mockStream(page, snapshot);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: async (text: string) => {
          (window as unknown as { copied: string }).copied = text;
        },
      },
      configurable: true,
    });
  });
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  await page.getByRole('button', { name: 'SHOW ALL DETAILS' }).click();
  const row = page.getByRole('region', {
    name: 'Session Build check',
    exact: true,
  });
  await expect(row).toContainText(session.text);
  await expect(row).toContainText('COMMAND // RUNNING');
  await expect(row).toContainText('+1 RUNNING');
  await expect(row.locator('pre.command')).toHaveText('go test ./...');
  await row.getByRole('button', { name: 'Copy text' }).click();
  await expect
    .poll(() =>
      page.evaluate(() => (window as unknown as { copied: string }).copied),
    )
    .toBe(session.text);
  session.text = '';
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await row.getByRole('button', { name: 'Copy text' }).click();
  await expect
    .poll(() =>
      page.evaluate(() => (window as unknown as { copied: string }).copied),
    )
    .toBe('go test ./...');
  await row.getByRole('button', { name: '-ROOT // Build check' }).click();
  await expect(page.locator('pre.command')).toHaveText('go test ./...');
  session.workingCommand = 'go vet ./...';
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await page.keyboard.press('c');
  await expect
    .poll(() =>
      page.evaluate(() => (window as unknown as { copied: string }).copied),
    )
    .toBe('go vet ./...');
  session.text = 'Checking the build <not markup>';
  session.commandStatus = 'completed';
  session.runningCommands = 0;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(
    page.getByRole('heading', { name: 'COMMAND // COMPLETED', exact: true }),
  ).toBeVisible();
  await expect(page.getByText(session.text, { exact: true })).toBeVisible();
  session.status = 'TURN COMPLETE';
  session.contextKind = 'LAST REPLY';
  session.text = 'Build finished';
  session.workingCommand = '';
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(page.getByText('Build finished', { exact: true })).toBeVisible();
  await expect(page.locator('pre.command')).toHaveCount(0);
});

test('session names identify selectable telemetry and full detail', async ({
  page,
  pairingURL,
}) => {
  await mockStream(page, {
    control: false,
    sessionsAt: new Date().toISOString(),
    meters: [],
    credits: [],
    sessions: [
      {
        id: 'named-root',
        name: 'Repair dashboard',
        directory: '/work/dashboard',
        tokens: 12,
        agents: 0,
        status: 'INPUT NEEDED',
        contextKind: 'LAST REPLY',
        text: 'Done',
        command: '',
        source: 'LOCAL',
        activity: '',
        samples: [],
      },
    ],
  });
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  const row = page.getByRole('region', {
    name: 'Session Repair dashboard',
    exact: true,
  });
  await expect(
    row.getByRole('heading', { name: '-ROOT // Repair dashboard' }),
  ).toBeVisible();
  await expect(row.locator('.telemetry')).toContainText('/work/dashboard');
  await row.getByRole('button', { name: '-ROOT // Repair dashboard' }).click();
  await expect(
    page.getByRole('navigation', { name: 'Sessions needing attention' }),
  ).toContainText('INPUT NEEDED -ROOT // Repair dashboard');
  await row.getByRole('link', { name: 'INPUT NEEDED', exact: true }).click();
  await expect(page.locator('.detail-heading')).toContainText(
    'Repair dashboard',
  );
  await expect(page.locator('.full-detail')).toContainText('/work/dashboard');
});

test('Thresholds navigation stays hidden without a launch policy', async ({
  page,
  pairingURL,
}) => {
  await page.goto(pairingURL);
  await expect(
    page.getByRole('link', { name: 'THRESHOLDS', exact: true }),
  ).toHaveCount(0);
});

test('read-only session copy captures working prose in every detail level without server writes', async ({
  page,
  pairingURL,
}) => {
  const text =
    'Visible activity\n' + 'offscreen line\n'.repeat(100) + 'LAST LINE';
  const snapshot = {
    control: false,
    sessionsAt: new Date().toISOString(),
    sessions: ['one', 'two'].map((id) => ({
      id,
      directory: id,
      tokens: 1,
      agents: 0,
      status: 'WORKING',
      contextKind: 'LAST ACTIVITY',
      text: id === 'one' ? text : 'Other session',
      command: '',
      source: 'LOCAL',
      activity: '',
      samples: [],
    })),
    meters: [],
    credits: [],
    creditCount: 0,
    usage: null,
  };
  await mockStream(page, snapshot);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: async (text: string) => {
          (window as unknown as { copied: string[] }).copied.push(text);
        },
      },
      configurable: true,
    });
    (window as unknown as { copied: string[] }).copied = [];
  });
  const actions: string[] = [];
  page.on('request', (req) => {
    if (req.url().includes('/api/control/')) actions.push(req.url());
  });
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  await page.getByRole('button', { name: 'SHOW ALL DETAILS' }).click();
  const row = page.getByRole('region', { name: 'Session one', exact: true });
  await row.getByRole('button', { name: 'Copy text' }).click();
  await expect(row.getByRole('status')).toHaveText('Copied.');
  await row.getByRole('button', { name: 'More detail' }).click();
  await row.getByRole('button', { name: 'Copy text' }).click();
  await page.keyboard.press('c');
  await row.getByRole('button', { name: 'More detail' }).click();
  await page
    .locator('.full-detail')
    .getByRole('button', { name: 'Copy text' })
    .click();
  await expect
    .poll(() =>
      page.evaluate(() => (window as unknown as { copied: string[] }).copied),
    )
    .toEqual([text, text, text, text]);
  expect(actions).toEqual([]);
  await page.evaluate(() => {
    location.hash = '/sessions/two';
  });
  await expect(page.locator('.full-detail pre').first()).toHaveText(
    'Other session',
  );
  await page.keyboard.press('c');
  await expect
    .poll(() =>
      page.evaluate(() =>
        (window as unknown as { copied: string[] }).copied.at(-1),
      ),
    )
    .toBe('Other session');
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: async () => {
          throw new Error('Denied');
        },
      },
    });
  });
  await page
    .locator('.full-detail')
    .getByRole('button', { name: 'Copy text' })
    .click();
  await expect(page.getByRole('status')).toHaveText(
    'Clipboard unavailable. Select the visible text and copy it manually.',
  );
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    {
      ...snapshot,
      sessions: snapshot.sessions.map((s) => ({
        ...s,
        contextKind: 'APPROVAL REQUEST',
        status: 'APPROVAL NEEDED',
      })),
    },
  );
  await expect(page.getByRole('button', { name: 'Copy text' })).toHaveCount(0);
  expect(actions).toEqual([]);
});

test('web copy does not steal composer typing or native copy shortcuts', async ({
  page,
  pairingURL,
}) => {
  await mockActions(page, 'prompt');
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: async () => {
          throw new Error('Must not copy');
        },
      },
    });
  });
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const editor = page.locator('textarea').first();
  await editor.fill('');
  await editor.press('c');
  await expect(editor).toHaveValue('c');
  await editor.press('Control+c');
  await expect(page.locator('.session-copy [role="status"]')).toBeEmpty();
});

test.describe('opt-in real server with simulated Codex actions', () => {
  test.use({ controlMode: true });
  test('demo approval completes through pairing, prepare and commit', async ({
    page,
    pairingURL,
  }) => {
    await page.goto(pairingURL);
    await expect(page.locator('header')).toContainText('SESSION CONTROL');
    await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
    await page.getByRole('link', { name: 'FULL DETAIL →' }).first().click();
    await page
      .getByRole('radio', { name: 'APPROVE ONCE', exact: true })
      .check();
    await page.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
    await page.getByRole('button', { name: 'CONFIRM APPROVE ONCE' }).click();
    await expect(page.locator('.full-detail')).toContainText(
      'Simulated decision: accept. No command was executed.',
    );
    await expect(page.getByRole('radio')).toHaveCount(0);
  });
});

// Browser contract tests use synthetic actions. The Go tests exercise the real
// authorization/confirmation endpoints with fake Codex clients, never live work.
async function mockActions(page: Page, kind = 'approval') {
  const snapshot = {
    control: true,
    version: 'test',
    meters: [],
    credits: [],
    creditCount: 0,
    usage: null,
    quotaAt: '',
    sessionsAt: new Date().toISOString(),
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    sessions: ['parent', 'other'].map((id) => ({
      id,
      directory: '/project/' + id,
      tokens: 100,
      agents: 0,
      status: kind === 'approval' ? 'APPROVAL NEEDED' : 'TURN COMPLETE',
      contextKind: 'LAST REPLY',
      text: 'Synthetic test context',
      command: '',
      source: 'APP SERVER',
      activity: '',
      samples: [],
    })),
  };
  const offer = {
    id: 'offer-1',
    kind,
    session: 'parent',
    thread: 'child',
    directory: '/project/child',
    command: kind === 'approval' ? 'git status' : '',
    choices: [
      { label: 'APPROVE ONCE', detail: '', persistent: false },
      { label: 'ALLOW FOR SESSION', detail: '', persistent: true },
    ],
    questions: [] as {
      text: string;
      secret: boolean;
      freeText: boolean;
      options: string[];
    }[],
  };
  const calls: { action: string; body: Record<string, unknown> }[] = [];
  await mockStream(page, snapshot);
  await page.route('**/api/control/*', async (route) => {
    const action = route.request().url().split('/').pop()!;
    const body = route.request().postDataJSON();
    if (action === 'schedules') {
      await route.fulfill({ json: [] });
      return;
    }
    calls.push({ action, body });
    const result =
      action === 'offer'
        ? { ...offer, session: body.session }
        : action === 'prepare'
          ? {
              confirmation: 'confirm-1',
              expires: new Date(Date.now() + 30000).toISOString(),
            }
          : { message: 'Sent' };
    await route.fulfill({ json: result });
  });
  return { snapshot, offer, calls };
}

test('file approval renders numbered red/green diff and requires confirmation', async ({
  page,
  pairingURL,
}) => {
  const { offer, calls, snapshot } = await mockActions(page);
  const fileChanges = [
    { text: 'UPDATE // /project/a.ts', kind: 'heading' },
    { text: '-old <script>alert(1)</script>', kind: 'removal', old: 3 },
    { text: '+new', kind: 'addition', new: 3 },
  ];
  Object.assign(offer, { command: '', fileChanges });
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const controls = page.getByRole('region', { name: 'Session controls' });
  await expect(controls.locator('.file-diff')).toContainText('/project/a.ts');
  await expect(controls.locator('.diff-summary')).toHaveText('+1 / −1');
  await expect(controls.locator('.diff-line.removed')).toContainText('3');
  await expect(controls.locator('.diff-line.removed')).toContainText(
    '<script>alert(1)</script>',
  );
  await expect(controls.locator('.diff-line.added')).toContainText('3');
  await expect(controls.locator('.diff-line.added')).toHaveCSS(
    'color',
    'rgb(103, 211, 145)',
  );
  await expect(controls.locator('.diff-line.removed')).toHaveCSS(
    'color',
    'rgb(255, 107, 131)',
  );
  await controls
    .getByRole('radio', { name: 'APPROVE ONCE', exact: true })
    .check();
  await controls.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
  await expect(controls).toContainText('proposed file changes above');
  expect(calls.filter((c) => c.action === 'commit')).toHaveLength(0);
  await controls.getByRole('button', { name: 'CONFIRM APPROVE ONCE' }).click();
  expect(calls.filter((c) => c.action === 'commit')).toHaveLength(1);
  // The same patch remains readable without enabling browser writes.
  snapshot.control = false;
  Object.assign(snapshot.sessions[0], { fileChanges });
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(page.locator('.detail-context .file-diff')).toBeVisible();
  await expect(page.locator('.detail-context .diff-summary')).toHaveText(
    '+1 / −1',
  );
  await expect(
    page.getByRole('region', { name: 'Session controls' }),
  ).toHaveCount(0);
});

test('scheduled follow-up is reviewed, visible and cancellable without sending immediately', async ({
  page,
  pairingURL,
}) => {
  const { calls } = await mockActions(page, 'prompt');
  let jobs: object[] = [];
  await page.route('**/api/control/schedules', async (route) => {
    if (route.request().postDataJSON().cancelId) jobs = [];
    await route.fulfill({ json: jobs });
  });
  await page.route('**/api/control/commit', async (route) => {
    jobs = [
      {
        id: 'job1',
        session: 'parent',
        text: 'Continue after quota recovery',
        trigger: 'quota',
        status: 'pending',
      },
    ];
    await route.fulfill({ json: { message: 'scheduled' } });
  });
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  await page
    .getByRole('textbox', { name: 'Follow-up message' })
    .fill('Continue after quota recovery');
  await page
    .getByRole('combobox', { name: 'Send', exact: true })
    .selectOption('quota');
  await page.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
  expect(calls.find((c) => c.action === 'prepare')?.body).toMatchObject({
    schedule: { trigger: 'quota' },
    answers: ['Continue after quota recovery'],
  });
  await expect(
    page.getByText(
      'TRIGGER // After fresh quota becomes available, when the session is idle',
    ),
  ).toBeVisible();
  await page.getByRole('button', { name: 'CONFIRM SCHEDULE' }).click();
  const pending = page.getByRole('region', { name: 'Pending follow-up' });
  await expect(pending).toContainText('Continue after quota recovery');
  await expect(
    page.getByRole('textbox', { name: 'Follow-up message' }),
  ).toHaveCount(0);
  await pending
    .getByRole('button', { name: 'EDIT · Ctrl+S', exact: true })
    .click();
  await expect(
    page.getByRole('textbox', { name: 'Follow-up message' }),
  ).toHaveValue('Continue after quota recovery');
  const draft = page.getByRole('textbox', { name: 'Follow-up message' });
  await draft.fill('Unsaved scheduling edit');
  await draft.press('Control+s');
  await expect(draft).toHaveValue('Unsaved scheduling edit');
  await expect(draft).toBeFocused();
  await expect(
    page.getByText('EDIT SCHEDULED FOLLOW-UP', { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole('button', { name: 'REVIEW CHANGES', exact: true }),
  ).toBeEnabled();
  await expect(pending.locator('pre')).toHaveText(
    'Continue after quota recovery',
  );
  await page
    .getByRole('button', { name: 'REVIEW CHANGES', exact: true })
    .click();
  expect(
    calls.filter((c) => c.action === 'prepare').at(-1)?.body,
  ).toMatchObject({ editId: 'job1', answers: ['Unsaved scheduling edit'] });
  jobs = [
    {
      id: 'job2',
      session: 'parent',
      text: 'Newer trigger',
      trigger: 'quota',
      status: 'pending',
    },
  ];
  await expect(
    page.getByText(
      'Trigger changed, removed or already sent. Go back and reopen it; your draft has not been saved.',
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    page.getByRole('button', { name: 'REVIEW CHANGES', exact: true }),
  ).toBeDisabled();
  await expect(draft).toHaveValue('Unsaved scheduling edit');
  await expect(
    page.getByRole('button', { name: 'CONFIRM SAVE CHANGES' }),
  ).toHaveCount(0);
  await pending.getByRole('button', { name: 'DELETE TRIGGER' }).click();
  await expect(pending).toHaveCount(0);
});

test('native approval detail omits unrelated scheduled prompt', async ({
  page,
  pairingURL,
}) => {
  await mockActions(page, 'approval');
  await page.route('**/api/control/schedules', (route) =>
    route.fulfill({
      json: [
        {
          id: 'job1',
          session: 'parent',
          text: 'Unrelated scheduled prompt',
          trigger: 'quota',
          status: 'pending',
        },
      ],
    }),
  );
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  await expect(
    page.getByRole('button', { name: 'REVIEW BEFORE SENDING' }),
  ).toBeVisible();
  await expect(
    page.getByRole('region', { name: 'Pending follow-up' }),
  ).toHaveCount(0);
  await expect(
    page.getByText('Unrelated scheduled prompt', { exact: true }),
  ).toHaveCount(0);
});

test('schedule form explains invalid input and previews exact timing', async ({
  page,
  pairingURL,
}) => {
  await mockActions(page, 'prompt');
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const timing = page.getByRole('combobox', { name: 'Send', exact: true });
  await timing.selectOption('delay');
  const review = page.getByRole('button', { name: 'REVIEW BEFORE SENDING' });
  await expect(review).toBeDisabled();
  await expect(
    page.getByText('Enter a message to schedule.', { exact: true }),
  ).toBeVisible();
  await page
    .getByRole('textbox', { name: 'Follow-up message' })
    .fill('Continue later');
  await page.getByRole('spinbutton', { name: 'Delay (minutes)' }).fill('0');
  await expect(review).toBeDisabled();
  await expect(
    page.getByText('Choose a future date/time or a delay greater than zero.', {
      exact: true,
    }),
  ).toBeVisible();
  await page.getByRole('spinbutton', { name: 'Delay (minutes)' }).fill('30');
  await expect(review).toBeEnabled();
  await expect(page.getByText(/^Will send:/)).toContainText(
    String(new Date().getFullYear()),
  );
  await timing.selectOption('at');
  await expect(review).toBeDisabled();
  await timing.selectOption('quota');
  await expect(review).toBeEnabled();
  await expect(page.getByText(/^Will send:/)).toContainText(
    'when quota is available and this session is idle',
  );
});

test('trigger pill and row link open detail; send now confirms the saved job', async ({
  page,
  pairingURL,
}) => {
  const { snapshot, calls } = await mockActions(page, 'prompt');
  let jobs: object[] = [
    {
      id: 'job1',
      session: 'parent',
      text: 'Saved job text',
      trigger: 'at',
      at: new Date(Date.now() + 3600000).toISOString(),
      status: 'pending',
      canSend: true,
    },
  ];
  let sends = 0;
  await page.route('**/api/control/schedules', (route) =>
    route.fulfill({ json: jobs }),
  );
  await page.route('**/api/control/commit', (route) => {
    sends++;
    jobs = [];
    return route.fulfill({ json: { message: 'Sent' } });
  });
  await page.goto(pairingURL);
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    { ...snapshot, triggers: [{ session: 'parent', status: 'pending' }] },
  );
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  await expect(
    page
      .locator('.telemetry')
      .getByRole('link', { name: 'TRIGGER SET', exact: true }),
  ).toHaveCount(1);
  await expect(
    page
      .locator('.telemetry')
      .getByRole('link', { name: 'TURN COMPLETE', exact: true })
      .and(page.locator('[href="#/sessions/parent"]')),
  ).toHaveCount(0);
  await page
    .getByRole('navigation', { name: 'Sessions needing attention' })
    .getByRole('link', { name: /TRIGGER SET/ })
    .click();
  const text = page.getByRole('textbox', { name: 'Follow-up message' });
  await expect(text).toHaveCount(0);
  await expect(
    page.getByRole('region', { name: 'Pending follow-up' }),
  ).toContainText('Saved job text');
  await expect(page.locator('.full-detail .detail-heading')).toContainText(
    'TRIGGER SET',
  );
  await expect(
    page.getByRole('region', { name: 'Pending follow-up' }),
  ).not.toContainText('TRIGGER SET');
  await page.getByRole('link', { name: '← ALL SESSIONS' }).click();
  await page
    .locator('.telemetry')
    .getByRole('link', { name: 'TRIGGER SET', exact: true })
    .click();
  await expect(text).toHaveCount(0);
  await expect(
    page.getByRole('region', { name: 'Pending follow-up' }),
  ).toContainText('Saved job text');
  await page.getByRole('button', { name: 'SEND NOW', exact: true }).click();
  expect(sends).toBe(0);
  expect(
    calls.filter((c) => c.action === 'prepare').at(-1)?.body,
  ).toMatchObject({ sendId: 'job1', answers: ['Saved job text'] });
  await page
    .getByRole('button', { name: 'CONFIRM SEND NOW', exact: true })
    .click();
  await expect(
    page.getByRole('region', { name: 'Pending follow-up' }),
  ).toHaveCount(0);
  expect(sends).toBe(1);
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    { ...snapshot, triggers: [] },
  );
  await page.getByRole('link', { name: '← ALL SESSIONS' }).click();
  await expect(
    page
      .locator('.telemetry')
      .getByRole('link', { name: 'TURN COMPLETE', exact: true })
      .and(page.locator('[href="#/sessions/parent"]')),
  ).toHaveCount(1);
  await expect(
    page
      .locator('.telemetry')
      .getByRole('link', { name: 'TRIGGER SET', exact: true }),
  ).toHaveCount(0);
});

for (const control of [false, true]) {
  test(`approval commentary is full-detail-only (control=${control})`, async ({
    page,
    pairingURL,
  }) => {
    const { snapshot } = await mockActions(page);
    const detail = {
      ...snapshot,
      control,
      sessions: snapshot.sessions.map((s) => ({
        ...s,
        contextKind: 'APPROVAL REQUEST',
        text: 'Allow this check?',
        approvalContext: 'I am checking <the build> before publishing.',
      })),
    };
    await page.goto(pairingURL);
    await page.evaluate(
      (detail) =>
        window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
      detail,
    );
    await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
    await page.getByRole('button', { name: 'SHOW ALL DETAILS' }).click();
    await expect(
      page.getByText(detail.sessions[0].approvalContext, { exact: true }),
    ).toHaveCount(0);
    await page.evaluate(() => {
      location.hash = '/sessions/parent';
    });
    const panel = page.locator('.detail-context');
    await expect(
      panel.getByRole('heading', { name: 'CONTEXT', exact: true }),
    ).toBeVisible();
    await expect(
      panel.getByText(detail.sessions[0].approvalContext, { exact: true }),
    ).toBeVisible();
    await expect(
      panel.getByText('Allow this check?', { exact: true }),
    ).toBeVisible();
    await expect(panel.locator('pre').first()).toHaveText(
      detail.sessions[0].approvalContext,
    );
    detail.sessions[0].approvalContext = '';
    await page.evaluate(
      (detail) =>
        window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
      detail,
    );
    await expect(
      panel.getByRole('heading', { name: 'CONTEXT', exact: true }),
    ).toHaveCount(0);
    await expect(
      panel.getByText('Allow this check?', { exact: true }),
    ).toBeVisible();
  });
}

test('session approval requires explicit review and confirmation of the target', async ({
  page,
  pairingURL,
}) => {
  const { calls, offer, snapshot } = await mockActions(page);
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const controls = page.getByRole('region', { name: 'Session controls' });
  await expect(controls).toContainText('TARGET // child // /project/child');
  await expect(controls.getByText('git status', { exact: true })).toBeVisible();
  await controls
    .getByRole('radio', { name: 'APPROVE ONCE', exact: true })
    .check();
  expect(calls.filter((c) => c.action !== 'offer')).toHaveLength(0);
  await controls.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
  await expect(
    controls.getByRole('button', { name: 'CONFIRM APPROVE ONCE' }),
  ).toBeVisible();
  expect(calls.filter((c) => c.action === 'commit')).toHaveLength(0);
  await controls.getByRole('button', { name: 'CONFIRM APPROVE ONCE' }).click();
  await expect(controls).toContainText('Decision sent.');
  expect(calls.filter((c) => c.action === 'commit')).toEqual([
    {
      action: 'commit',
      body: { session: 'parent', offer: 'offer-1', confirmation: 'confirm-1' },
    },
  ]);
  await expect(controls.getByRole('radio')).toHaveCount(0);
  offer.id = '';
  snapshot.sessions[0].status = 'WORKING';
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(controls).toContainText(
    'Codex is working — nothing to respond to.',
  );
  await expect(controls).not.toContainText('Respond in Codex');
  await expect(controls).not.toContainText('shared app-server');
  await expect(controls.locator('.notice')).toHaveCount(0);
  snapshot.sessionsError = true;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(controls).toContainText(
    'Session controls temporarily unavailable',
  );
});

for (const status of [400, 401, 403, 404, 409, 502]) {
  test(`HTTP ${status} is classified correctly after commit`, async ({
    page,
    pairingURL,
  }) => {
    await mockActions(page);
    await page.route('**/api/control/commit', (route) =>
      route.fulfill({ status, body: 'rejected' }),
    );
    await page.goto(pairingURL);
    await page.evaluate(() => {
      location.hash = '/sessions/parent';
    });
    await page
      .getByRole('radio', { name: 'APPROVE ONCE', exact: true })
      .check();
    await page.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
    await page.getByRole('button', { name: 'CONFIRM APPROVE ONCE' }).click();
    const notice = page
      .getByRole('region', { name: 'Session controls' })
      .getByRole('status');
    await expect(notice).toContainText(
      status === 502
        ? 'Outcome uncertain'
        : 'Action rejected, expired or changed',
    );
  });
}

test('approval navigation matches each terminal theme warning colour and stays clickable', async ({
  page,
  pairingURL,
}) => {
  await mockActions(page);
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  const button = page
    .getByRole('navigation', { name: 'Sessions needing attention' })
    .getByRole('link')
    .first();
  for (const [theme, colour] of [
    ['hacker', 'rgb(255, 202, 88)'],
    ['rust', 'rgb(255, 138, 61)'],
    ['blue-steel', 'rgb(232, 196, 106)'],
    ['ultraviolet', 'rgb(249, 168, 212)'],
    ['nightshade', 'rgb(143, 124, 255)'],
  ]) {
    await page.getByLabel('Theme', { exact: true }).selectOption(theme);
    await expect(button).toHaveCSS('color', colour);
    await expect(button).toHaveCSS('border-top-color', colour);
    await button.hover();
    await expect(button).toHaveCSS('color', colour);
  }
  await button.click();
  await expect(page).toHaveURL(/sessions\/parent$/);
});

test('detail retains totals and one command in a single column at all widths', async ({
  page,
  pairingURL,
}) => {
  const { snapshot, offer } = await mockActions(page);
  snapshot.sessions[0].command = 'git status';
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  await expect(
    page.getByRole('heading', { name: 'SESSION TOTALS', exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole('radio', { name: 'APPROVE ONCE', exact: true }),
  ).toBeVisible();
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(page.locator('.full-detail pre.command')).toHaveCount(1);
  const context = page.locator('.detail-context');
  const actions = page.getByRole('region', { name: 'Session controls' });
  const left = (await context.boundingBox())!;
  const right = (await actions.boundingBox())!;
  expect(right.y).toBeGreaterThanOrEqual(left.y + left.height);
  expect(Math.abs(right.x - left.x)).toBeLessThan(2);
  expect(Math.abs(right.width - left.width)).toBeLessThan(2);
  await page.screenshot({ path: 'test-results/detail-workspace-wide.png' });
  await page.setViewportSize({ width: 390, height: 800 });
  const above = (await context.boundingBox())!;
  const below = (await actions.boundingBox())!;
  expect(below.y).toBeGreaterThanOrEqual(above.y + above.height);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  offer.id = '';
  await expect(
    actions.getByRole('heading', {
      name: 'LAST OBSERVED COMMAND',
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.locator('.full-detail pre.command')).toHaveCount(1);
  await expect(actions.locator('pre.command')).toHaveText('git status');
});

test('changed requests, stale data and navigation invalidate browser confirmation', async ({
  page,
  pairingURL,
}) => {
  const { snapshot, offer, calls } = await mockActions(page);
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  await page.getByRole('radio', { name: /ALLOW FOR SESSION/ }).check();
  await page.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
  await expect(
    page.getByRole('button', { name: 'CONFIRM ALLOW FOR SESSION' }),
  ).toBeVisible();
  offer.id = 'offer-2';
  offer.command = 'git diff';
  await expect(
    page.getByRole('button', { name: 'CONFIRM ALLOW FOR SESSION' }),
  ).toHaveCount(0);
  await expect(
    page.getByRole('radio', { name: /ALLOW FOR SESSION/ }),
  ).not.toBeChecked();
  await page.getByRole('radio', { name: /APPROVE ONCE/ }).check();
  await page.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
  snapshot.sessionsError = true;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(page.getByRole('button', { name: /CONFIRM/ })).toHaveCount(0);
  snapshot.sessionsError = false;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await page.evaluate(() => {
    location.hash = '/sessions/other';
  });
  await expect(page.getByRole('button', { name: /CONFIRM/ })).toHaveCount(0);
  expect(calls.filter((c) => c.action === 'commit')).toHaveLength(0);
});

test('slash commands discover help and require a separate confirmation', async ({
  page,
  pairingURL,
}) => {
  const { calls } = await mockActions(page, 'prompt');
  const commands: string[] = [];
  let revision = 'r0';
  let rejectCommit = false;
  await page.route('**/api/control/commands', async (route) => {
    const body = route.request().postDataJSON();
    commands.push(body.command.mode);
    let result: object;
    if (body.command.mode === 'prepare') {
      // Applying a setting invalidates the old catalogue, as the real server does.
      if (body.command.revision !== revision) {
        await route.fulfill({
          status: 409,
          json: { error: 'Command unavailable or unconfirmed' },
        });
        return;
      }
      result = {
        confirmation: 'command-token',
        expires: new Date(Date.now() + 30000).toISOString(),
      };
    } else if (body.command.mode === 'commit') {
      if (rejectCommit) {
        await route.fulfill({
          status: 409,
          json: { error: 'Command unavailable or unconfirmed' },
        });
        return;
      }
      revision = 'r1';
      result = {
        message: 'Change requested. Codex will apply it to subsequent turns.',
      };
    } else
      result =
        body.command.path === ''
          ? {
              title: '/ COMMANDS',
              help: 'Live options',
              path: '',
              revision,
              choices: [
                {
                  id: 'm',
                  label: '/model',
                  help: 'Choose a model. '.repeat(50),
                  next: 'model/test',
                },
              ],
            }
          : {
              title: '/model/test',
              help: 'Server model help',
              path: 'model/test',
              revision,
              choices: [
                {
                  id: 'effort',
                  label: 'Medium',
                  help: 'Server effort description',
                  action: true,
                },
                {
                  id: 'high',
                  label: 'High',
                  help: 'Higher effort',
                  action: true,
                },
              ],
            };
    await route.fulfill({ json: result });
  });
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const text = page
    .locator('.detail-workspace')
    .getByRole('textbox', { name: 'Follow-up message' });
  await text.fill('/');
  const panel = page.getByRole('region', { name: 'Session slash commands' });
  await expect(
    panel.getByRole('button', { name: '/model →', exact: true }),
  ).toBeEnabled();
  const popup = panel.locator('.command-popup');
  await expect(popup).toBeVisible();
  const initialPopup = await popup.boundingBox();
  const initialComposer = await text.boundingBox();
  const initialOptions = await popup.locator('.command-options button').all();
  expect(initialOptions).toHaveLength(2);
  const firstOption = await initialOptions[0].boundingBox();
  const secondOption = await initialOptions[1].boundingBox();
  expect(secondOption!.y).toBeGreaterThanOrEqual(
    firstOption!.y + firstOption!.height,
  );
  const helpStyle = await initialOptions[0]
    .locator('.command-help')
    .evaluate((node) => {
      const style = getComputedStyle(node);
      const command = node.parentElement!.querySelector('.command-name')!;
      return {
        whiteSpace: style.whiteSpace,
        overflow: style.overflow,
        ellipsis: style.textOverflow,
        clipped: node.scrollWidth > node.clientWidth,
        helpBackground: style.backgroundColor,
        commandBackground: getComputedStyle(command).backgroundColor,
        sameLine:
          Math.abs(
            node.getBoundingClientRect().y - command.getBoundingClientRect().y,
          ) < 5,
      };
    });
  expect(helpStyle).toMatchObject({
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    ellipsis: 'ellipsis',
    clipped: true,
    sameLine: true,
  });
  expect(helpStyle.helpBackground).not.toBe(helpStyle.commandBackground);
  await panel.getByRole('button', { name: '/ COMMANDS', exact: true }).click();
  await expect(
    panel.getByRole('button', { name: '/model →', exact: true }),
  ).toHaveCount(0);
  await panel.getByRole('button', { name: '/ COMMANDS', exact: true }).click();
  await expect(
    panel.getByRole('button', { name: '/model →', exact: true }),
  ).toBeEnabled();
  await expect(
    panel.getByRole('button', { name: '/model →', exact: true }),
  ).toBeEnabled();
  await text.fill('/mo');
  await expect(popup.locator('.command-options button')).toHaveCount(1);
  const filteredPopup = await popup.boundingBox();
  const filteredComposer = await text.boundingBox();
  expect(filteredPopup!.height).toBeLessThan(initialPopup!.height);
  expect(filteredComposer!.y).toBe(initialComposer!.y);
  await text.press('Tab');
  await expect(text).toHaveValue('/model');
  await text.press('Escape');
  await expect(
    panel.getByRole('button', { name: '/model →', exact: true }),
  ).toHaveCount(0);
  await expect(text).toBeFocused();
  await text.fill('/mod');
  await expect(
    panel.getByRole('button', { name: '/model →', exact: true }),
  ).toBeEnabled();
  await text.press('Enter');
  const medium = panel.getByRole('button', { name: 'Medium', exact: true });
  const high = panel.getByRole('button', { name: 'High', exact: true });
  await expect(medium).toHaveClass(/suggested/);
  await expect(high).not.toHaveClass(/suggested/);
  const selectedStyle = await medium
    .locator('.command-name')
    .evaluate((node) => ({
      color: getComputedStyle(node).color,
      background: getComputedStyle(node).backgroundColor,
      weight: getComputedStyle(node).fontWeight,
    }));
  const unselectedStyle = await high
    .locator('.command-name')
    .evaluate((node) => ({
      color: getComputedStyle(node).color,
      background: getComputedStyle(node).backgroundColor,
    }));
  expect(selectedStyle.background).not.toBe(unselectedStyle.background);
  expect(selectedStyle.color).not.toBe(unselectedStyle.color);
  expect(Number(selectedStyle.weight)).toBeGreaterThanOrEqual(700);
  await text.press('ArrowDown');
  await expect(high).toHaveClass(/suggested/);
  await expect(medium).not.toHaveClass(/suggested/);
  await medium.focus();
  await medium.press('c');
  expect(
    commands.filter((c) => c === 'prepare' || c === 'commit'),
  ).toHaveLength(0);
  await medium.press('Enter');
  await expect(panel).toContainText('Server effort description');
  await expect(
    panel.getByRole('button', { name: 'CONFIRM CHANGE' }),
  ).toBeEnabled();
  expect(commands.filter((c) => c === 'commit')).toHaveLength(0);
  const review = panel.getByRole('group', { name: 'Review command change' });
  await expect(review).toBeFocused();
  await review.press('Enter');
  await review.press('Enter');
  expect(commands.filter((c) => c === 'commit')).toHaveLength(0);
  await review.press('c');
  await expect(panel).toContainText('Change requested.');
  expect(commands.filter((c) => c === 'commit')).toHaveLength(1);
  expect(calls.filter((c) => c.action === 'commit')).toHaveLength(0);
  await expect(text).toHaveValue('');
  await expect(text).toBeFocused();
  await expect(
    panel.getByRole('button', { name: 'BACK', exact: true }),
  ).toHaveCount(0);
  await expect(
    panel.getByRole('button', { name: 'Medium', exact: true }),
  ).toHaveCount(0);
  await expect(
    panel.getByRole('button', { name: 'REFRESH OPTIONS', exact: true }),
  ).toHaveCount(0);
  // Repeating the same command must reopen a fresh catalogue, not the old revision.
  await text.fill('/mod');
  await expect(
    panel.getByRole('button', { name: '/model →', exact: true }),
  ).toBeEnabled();
  await text.press('Enter');
  await panel.getByRole('button', { name: 'High', exact: true }).click();
  await panel.getByRole('button', { name: 'CONFIRM CHANGE' }).click();
  await expect(text).toHaveValue('');
  await expect(
    panel.getByRole('button', { name: 'High', exact: true }),
  ).toHaveCount(0);
  await expect(panel).not.toContainText('Change unconfirmed');
  expect(commands.filter((c) => c === 'commit')).toHaveLength(2);
  // A failed confirmation retains the user's draft and the error context.
  rejectCommit = true;
  await text.fill('/mod');
  await expect(
    panel.getByRole('button', { name: '/model →', exact: true }),
  ).toBeEnabled();
  await text.press('Enter');
  await panel.getByRole('button', { name: 'Medium', exact: true }).click();
  await panel.getByRole('button', { name: 'CONFIRM CHANGE' }).click();
  await expect(panel).toContainText('Change unconfirmed');
  await expect(text).toHaveValue('/mod');
  await expect(
    panel.getByRole('button', { name: 'BACK', exact: true }),
  ).toBeVisible();
  await expect(
    panel.getByRole('button', { name: 'CONFIRM CHANGE' }),
  ).toBeDisabled();
});

for (const command of ['rename', 'cd']) {
  test(
    command + ' edits and reviews input before a separate confirmation',
    async ({ page, pairingURL }) => {
      const { calls } = await mockActions(page, 'prompt');
      const value =
        command === 'cd' ? '/new  directory 界' : 'quota / review 界';
      const label = command === 'cd' ? 'Working directory' : 'Session name';
      const message =
        command === 'cd' ? 'Working directory changed.' : 'Session renamed.';
      const modes: string[] = [];
      let reviewed = '';
      await page.route('**/api/control/commands', async (route) => {
        const body = route.request().postDataJSON();
        const { mode, path } = body.command;
        modes.push(mode);
        let result: object;
        if (mode === 'prepare') {
          expect(path).toBe(command + '/' + encodeURIComponent(value));
          result = {
            confirmation: command + '-token',
            expires: new Date(Date.now() + 30000).toISOString(),
          };
        } else if (mode === 'commit') {
          expect(body.confirmation).toBe(command + '-token');
          result = { message };
        } else if (path === '') {
          result = {
            title: '/ COMMANDS',
            help: 'Commands',
            path: '',
            revision: 'r',
            choices: [
              {
                id: 'rename',
                label: '/' + command,
                help: 'Rename the session',
                next: command,
              },
            ],
          };
        } else if (path === command) {
          result = {
            title: '/' + command,
            help: 'Enter a name, review and confirm.',
            path,
            revision: 'r',
            input: true,
            inputLabel: label,
            inputLimit: command === 'cd' ? 1024 : 512,
            value: 'Current name',
            choices: [],
          };
        } else {
          reviewed = decodeURIComponent(path.slice(command.length + 1));
          result = {
            title: '/' + command,
            help: 'Only the saved name changes.',
            path,
            revision: 'r',
            choices: [
              {
                id: 'name',
                label: 'Review ' + reviewed,
                help: 'New name: ' + reviewed,
                action: true,
              },
            ],
          };
        }
        await route.fulfill({ json: result });
      });
      await page.goto(pairingURL);
      await page.evaluate(() => {
        location.hash = '/sessions/parent';
      });
      const text = page
        .locator('.detail-workspace')
        .getByRole('textbox', { name: 'Follow-up message' });
      await text.fill('/' + command);
      const panel = page.getByRole('region', {
        name: 'Session slash commands',
      });
      await panel
        .getByRole('button', { name: '/' + command + ' →', exact: true })
        .click();
      const input = panel.getByRole('textbox', {
        name: label,
        exact: true,
      });
      await expect(input).toHaveValue('Current name');
      await input.fill(value);
      await panel
        .getByRole('button', {
          name: command === 'cd' ? 'REVIEW DIRECTORY' : 'REVIEW RENAME',
        })
        .click();
      await expect(
        panel.getByRole('heading', { name: 'Review ' + value }),
      ).toBeVisible();
      expect(reviewed).toBe(value);
      expect(modes.filter((m) => m === 'commit')).toHaveLength(0);
      await panel.getByRole('button', { name: 'CONFIRM CHANGE' }).click();
      await expect(
        panel.getByRole('button', { name: 'CONFIRM CHANGE' }),
      ).toHaveCount(0);
      expect(modes.filter((m) => m === 'commit')).toHaveLength(1);
      expect(calls.filter((call) => call.action === 'prepare')).toHaveLength(0);
    },
  );
}

async function mockSpeedCommands(page: Page) {
  await mockActions(page, 'prompt');
  const calls: { mode: string; path: string; choice?: string }[] = [];
  let reject = false;
  await page.route('**/api/control/commands', async (route) => {
    const body = route.request().postDataJSON();
    const command = body.command;
    calls.push(command);
    if (reject && command.mode === 'commit') {
      await route.fulfill({ status: 409, json: { error: 'Changed' } });
      return;
    }
    await route.fulfill({
      json:
        command.mode === 'prepare'
          ? {
              confirmation: 'speed-token',
              expires: new Date(Date.now() + 30000).toISOString(),
            }
          : command.mode === 'commit'
            ? { message: 'Session speed changed. Applies to subsequent turns.' }
            : command.path === ''
              ? {
                  title: '/ COMMANDS',
                  help: 'Live commands',
                  path: '',
                  revision: 'r',
                  choices: [
                    {
                      id: 'speed',
                      label: '/fast',
                      help: 'Choose speed',
                      next: 'tier/flex',
                    },
                  ],
                }
              : {
                  title: '/fast',
                  help: 'Choose session speed',
                  path: 'tier/flex',
                  revision: 'r',
                  picker: true,
                  choices: [
                    {
                      id: 'default',
                      label: 'Standard (default)',
                      help: 'Use the server default',
                      action: true,
                    },
                    {
                      id: 'flex',
                      label: 'Fast',
                      help: 'Advertised faster tier',
                      action: true,
                    },
                    {
                      id: 'priority',
                      label: 'Slow',
                      help: 'Advertised slower tier',
                      action: true,
                      selected: true,
                    },
                  ],
                },
    });
  });
  return {
    calls,
    rejectCommit: () => {
      reject = true;
    },
  };
}

async function openSpeedCommands(page: Page, pairingURL: string) {
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const text = page
    .locator('.detail-workspace')
    .getByRole('textbox', { name: 'Follow-up message' });
  await text.fill('/fast');
  const panel = page.getByRole('region', { name: 'Session slash commands' });
  await expect(
    panel.getByRole('button', { name: '/fast →', exact: true }),
  ).toBeEnabled();
  await text.press('Enter');
  const list = panel.getByRole('listbox', { name: 'Session speed' });
  await expect(list).toBeFocused();
  await expect(list.getByRole('option')).toHaveCount(3);
  return { text, panel, list };
}

test('speed picker keeps choices visible while Enter reviews and C confirms', async ({
  page,
  pairingURL,
}) => {
  const { calls } = await mockSpeedCommands(page);
  const { text, panel, list } = await openSpeedCommands(page, pairingURL);
  const slow = list.getByRole('option', {
    name: 'Slow // CURRENT',
    exact: true,
  });
  const fast = list.getByRole('option', { name: 'Fast', exact: true });
  await expect(slow).toHaveAttribute('aria-selected', 'true');
  await list.press('ArrowDown');
  await expect(slow).toHaveAttribute('aria-selected', 'true');
  await list.press('ArrowUp');
  await expect(fast).toHaveAttribute('aria-selected', 'true');
  await expect(slow).toHaveAttribute('aria-selected', 'false');
  await expect(slow).toContainText('CURRENT');
  await list.press('c');
  expect(calls.every((c) => c.mode === 'list')).toBe(true);
  await expect(
    panel.getByRole('button', { name: 'C CONFIRM CHANGE', exact: true }),
  ).toBeDisabled();
  await list.press('Enter');
  await expect(panel).toContainText('REVIEW // Fast');
  await expect(list).toBeVisible();
  await expect(list.getByRole('option')).toHaveCount(3);
  await expect(text).toHaveValue('/fast');
  expect(calls.filter((c) => c.mode === 'commit')).toHaveLength(0);
  await expect(
    panel.getByRole('button', { name: 'C CONFIRM CHANGE', exact: true }),
  ).toBeEnabled();
  await list.press('c');
  await expect(list).toHaveCount(0);
  await expect(panel).toContainText('Session speed changed.');
  await expect(text).toHaveValue('');
  await expect(text).toBeFocused();
  expect(calls.filter((c) => c.mode === 'prepare')).toEqual([
    { mode: 'prepare', path: 'tier/flex', revision: 'r', choice: 'flex' },
  ]);
  expect(calls.filter((c) => c.mode === 'commit')).toHaveLength(1);
});

test('speed picker footer reviews then confirms and Escape preserves the draft', async ({
  page,
  pairingURL,
}) => {
  const { calls } = await mockSpeedCommands(page);
  const { text, panel, list } = await openSpeedCommands(page, pairingURL);
  await list
    .getByRole('option', { name: 'Standard (default)', exact: true })
    .click();
  await expect(
    list.getByRole('option', { name: 'Standard (default)', exact: true }),
  ).toHaveAttribute('aria-selected', 'true');
  expect(calls.every((c) => c.mode === 'list')).toBe(true);
  await list.press('Escape');
  await expect(list).toHaveCount(0);
  await expect(text).toHaveValue('/fast');
  await expect(text).toBeFocused();
  await text.fill('/fas');
  await expect(
    panel.getByRole('button', { name: '/fast →', exact: true }),
  ).toBeEnabled();
  await text.press('Enter');
  await expect(list).toBeFocused();
  await list
    .getByRole('option', { name: 'Standard (default)', exact: true })
    .click();
  await panel
    .getByRole('button', { name: 'ENTER REVIEW', exact: true })
    .click();
  await expect(
    panel.getByRole('button', { name: 'C CONFIRM CHANGE', exact: true }),
  ).toBeEnabled();
  expect(calls.filter((c) => c.mode === 'commit')).toHaveLength(0);
  await panel
    .getByRole('button', { name: 'C CONFIRM CHANGE', exact: true })
    .click();
  await expect(list).toHaveCount(0);
  expect(
    calls.filter((c) => c.mode === 'prepare').map((c) => c.choice),
  ).toEqual(['default']);
  expect(calls.filter((c) => c.mode === 'commit')).toHaveLength(1);
});

test('changing a provisional speed cancels its confirmation', async ({
  page,
  pairingURL,
}) => {
  const { calls } = await mockSpeedCommands(page);
  const { panel, list } = await openSpeedCommands(page, pairingURL);
  const confirm = panel.getByRole('button', {
    name: 'C CONFIRM CHANGE',
    exact: true,
  });
  await list.press('Enter');
  await expect(confirm).toBeEnabled();
  await list.press('ArrowUp');
  await expect(confirm).toBeDisabled();
  await expect(panel).not.toContainText('REVIEW // Slow');
  await list.press('c');
  expect(calls.filter((c) => c.mode === 'commit')).toHaveLength(0);
  await list.press('Enter');
  await expect(confirm).toBeEnabled();
  // Enter on the confirmation button must still only review, never commit.
  await confirm.press('Enter');
  await expect(confirm).toBeEnabled();
  expect(calls.filter((c) => c.mode === 'commit')).toHaveLength(0);
  await confirm.press('c');
  await expect(list).toHaveCount(0);
  expect(
    calls.filter((c) => c.mode === 'prepare').map((c) => c.choice),
  ).toEqual(['priority', 'flex', 'flex']);
  expect(calls.filter((c) => c.mode === 'commit')).toHaveLength(1);
});

test('expired or cancelled speed reviews cannot commit', async ({
  page,
  pairingURL,
}) => {
  const { calls } = await mockSpeedCommands(page);
  const { panel, list, text } = await openSpeedCommands(page, pairingURL);
  const confirm = panel.getByRole('button', {
    name: 'C CONFIRM CHANGE',
    exact: true,
  });
  await list.press('Enter');
  await expect(confirm).toBeEnabled();
  await page.clock.setFixedTime(new Date(Date.now() + 60000));
  await list.press('c');
  expect(calls.filter((c) => c.mode === 'commit')).toHaveLength(0);
  await expect(confirm).toBeDisabled();
  await expect(panel).toContainText('Review expired');
  await page.clock.setFixedTime(new Date(Date.now()));
  await list.press('Enter');
  await expect(confirm).toBeEnabled();
  await list.press('Escape');
  await expect(list).toHaveCount(0);
  await expect(text).toHaveValue('/fast');
  await panel.getByRole('button', { name: '/ COMMANDS', exact: true }).click();
  await panel.getByRole('button', { name: '/fast →', exact: true }).click();
  await expect(list).toBeFocused();
  await expect(confirm).toBeDisabled();
  await list.press('c');
  expect(calls.filter((c) => c.mode === 'commit')).toHaveLength(0);
});

test('unconfirmed speed changes preserve the list and draft without retrying', async ({
  page,
  pairingURL,
}) => {
  const { calls, rejectCommit } = await mockSpeedCommands(page);
  rejectCommit();
  const { text, panel, list } = await openSpeedCommands(page, pairingURL);
  await list.press('ArrowUp');
  await list.press('Enter');
  await expect(
    panel.getByRole('button', { name: 'C CONFIRM CHANGE', exact: true }),
  ).toBeEnabled();
  await list.press('c');
  await expect(panel).toContainText('Change unconfirmed.');
  await expect(list.getByRole('option')).toHaveCount(3);
  await expect(text).toHaveValue('/fast');
  await expect(
    panel.getByRole('button', { name: 'C CONFIRM CHANGE', exact: true }),
  ).toBeDisabled();
  await list.press('c');
  expect(calls.filter((c) => c.mode === 'commit')).toHaveLength(1);
});

test('unavailable slash commands close without losing the draft and recover with a fresh catalogue', async ({
  page,
  pairingURL,
}) => {
  const { snapshot } = await mockActions(page, 'prompt');
  let lists = 0;
  const modes: string[] = [];
  await page.route('**/api/control/commands', async (route) => {
    const body = route.request().postDataJSON();
    modes.push(body.command.mode);
    if (body.command.mode === 'list') lists++;
    await route.fulfill({
      json: {
        title: '/ COMMANDS',
        help: 'Live options',
        path: '',
        revision: 'r' + lists,
        choices: [
          { id: 'model', label: '/model', help: 'Model help', next: 'model' },
        ],
      },
    });
  });
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const text = page
    .locator('.detail-workspace')
    .getByRole('textbox', { name: 'Follow-up message' });
  await text.fill('/mod');
  const panel = page.getByRole('region', { name: 'Session slash commands' });
  await expect(
    panel.getByRole('button', { name: '/model →', exact: true }),
  ).toBeVisible();
  const before = lists;
  snapshot.sessionsError = true;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(page.locator('.command-popup')).toHaveCount(0);
  await expect(
    page.getByText(
      'Session controls temporarily unavailable. Check Codex for current state.',
    ),
  ).toBeVisible();
  expect(lists).toBe(before);
  snapshot.sessionsError = false;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(text).toHaveValue('/mod');
  // Dismiss any newly mounted suggestions, then explicitly reopen commands.
  await text.press('Escape');
  await expect(page.locator('.command-popup')).toHaveCount(0);
  const recovered = lists;
  await panel.getByRole('button', { name: '/ COMMANDS', exact: true }).click();
  await expect(
    panel.getByRole('button', { name: '/model →', exact: true }),
  ).toBeVisible();
  expect(lists).toBeGreaterThan(recovered);
  await text.press('Escape');
  await expect(page.locator('.command-popup')).toHaveCount(0);
  await expect(text).toHaveValue('/mod');
  expect(modes.every((mode) => mode === 'list')).toBe(true);
});

test('read-only status line supports multiple fields, ordering and persistence without control writes', async ({
  page,
  pairingURL,
}) => {
  await mockStream(page, {
    version: 'test',
    control: false,
    meters: [],
    credits: [],
    creditCount: 0,
    usage: null,
    sessionsError: false,
    sessionsAt: new Date().toISOString(),
    statusLineFields: [
      {
        id: 'model-with-reasoning',
        label: 'Model and reasoning',
        help: 'Observed model and effort',
        default: true,
      },
      {
        id: 'used-tokens',
        label: 'Tokens',
        help: 'Observed tokens',
        default: true,
      },
      {
        id: 'thread-id',
        label: 'Session ID',
        help: 'Full identifier',
        default: false,
      },
    ],
    sessions: [
      {
        id: 'one',
        name: 'Test session',
        directory: '/work',
        tokens: 1200,
        agents: 0,
        status: 'TURN COMPLETE',
        contextKind: 'LAST REPLY',
        text: 'Done.',
        command: '',
        source: 'LOCAL',
        activity: new Date().toISOString(),
        samples: [],
        statusLine: {
          'model-with-reasoning': 'model high',
          'used-tokens': '1.2K tokens',
          'thread-id': 'one',
        },
      },
    ],
  });
  const writes: string[] = [];
  await page.route('**/api/control/**', (route) => {
    writes.push(route.request().url());
    return route.abort();
  });
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/one';
  });
  const line = page.getByLabel('Session status line', { exact: true });
  await expect(line).toHaveText('model high · 1.2K tokens');
  await page.getByRole('button', { name: 'Configure status line' }).click();
  const picker = page.getByRole('region', { name: 'Status line fields' });
  await picker.getByRole('checkbox', { name: /Model and reasoning/ }).uncheck();
  await picker.getByRole('checkbox', { name: /Session ID/ }).check();
  await picker.getByRole('button', { name: 'Move Session ID earlier' }).click();
  await expect(page.getByLabel('Status line preview')).toContainText(
    'one · 1.2K tokens',
  );
  await expect(line).toHaveText('model high · 1.2K tokens');
  await picker.getByRole('button', { name: 'APPLY', exact: true }).click();
  await expect(line).toHaveText('one · 1.2K tokens');
  await page.reload();
  await expect(line).toHaveText('one · 1.2K tokens');
  await page.getByRole('button', { name: 'Configure status line' }).click();
  await picker.getByRole('checkbox', { name: /Tokens/ }).uncheck();
  await picker.getByRole('button', { name: 'CANCEL', exact: true }).click();
  await expect(line).toHaveText('one · 1.2K tokens');
  expect(writes).toHaveLength(0);
});

test('quota review hides only follow-ups, cancels confirmation and preserves the draft', async ({
  page,
  pairingURL,
}) => {
  const { snapshot, offer, calls } = await mockActions(page, 'prompt');
  const state = {
    ...snapshot,
    profiles: [] as { session: string; pending: boolean }[],
  };
  const publish = () =>
    page.evaluate(
      (detail) =>
        window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
      state,
    );
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const native = page.locator('.detail-workspace');
  const text = native.getByRole('textbox', { name: 'Follow-up message' });
  await text.fill('Keep this draft');
  await native.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
  await expect(
    native.getByRole('button', { name: 'CONFIRM SEND' }),
  ).toBeVisible();
  state.profiles = [{ session: 'parent', pending: true }];
  await publish();
  await expect(
    native.getByRole('region', { name: 'Session controls' }),
  ).toHaveCount(0);
  state.profiles = [];
  await publish();
  await expect(text).toHaveValue('Keep this draft');
  await expect(
    native.getByRole('button', { name: 'CONFIRM SEND' }),
  ).toHaveCount(0);
  // Reviews for another session must not hide this session's composer.
  state.profiles = [{ session: 'other', pending: true }];
  await publish();
  await expect(text).toBeVisible();
  state.profiles = [{ session: 'parent', pending: true }];
  offer.id = 'question';
  offer.questions = [
    { text: 'Choose environment', secret: false, freeText: true, options: [] },
  ];
  await publish();
  await expect(
    native.getByRole('textbox', { name: 'Choose environment' }),
  ).toBeVisible();
  offer.id = 'approval';
  offer.kind = 'approval';
  offer.questions = [];
  await expect(
    native.getByRole('radio', { name: 'APPROVE ONCE', exact: true }),
  ).toBeVisible();
  expect(calls.filter((c) => c.action === 'commit')).toHaveLength(0);
});

test('quota pills cannot discard another session draft or in-flight send', async ({
  page,
  pairingURL,
}) => {
  const { snapshot } = await mockActions(page, 'prompt');
  const state = {
    ...snapshot,
    profiles: [{ session: 'other', pending: true }],
  };
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const text = page.getByRole('textbox', { name: 'Follow-up message' });
  await text.fill('Keep my draft');
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    state,
  );
  const pill = page
    .getByRole('navigation', { name: 'Sessions needing attention' })
    .getByRole('link', { name: /QUOTA THRESHOLD/ });
  await pill.click();
  await expect(page).toHaveURL(/sessions\/parent$/);
  await expect(text).toHaveValue('Keep my draft');
  let finish!: () => void;
  const pending = new Promise<void>((resolve) => {
    finish = resolve;
  });
  await page.route('**/api/control/commit', async (route) => {
    await pending;
    await route.fulfill({ json: { message: 'Sent' } });
  });
  await page.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
  await page.getByRole('button', { name: 'CONFIRM SEND' }).click();
  await pill.click();
  await expect(page).toHaveURL(/sessions\/parent$/);
  finish();
  await expect(
    page.getByRole('region', { name: 'Session controls' }),
  ).toContainText('Text sent.');
  await pill.click();
  await expect(page).toHaveURL(/sessions\/other\?review=profile$/);
});

test('follow-up drafts are scoped, keyboard-safe, confirmed and not persisted', async ({
  page,
  pairingURL,
}) => {
  const { calls } = await mockActions(page, 'prompt');
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const text = page.getByRole('textbox', { name: 'Follow-up message' });
  await text.fill('Synthetic private draft');
  await text.press('ArrowLeft');
  await text.press('Enter');
  await expect(page).toHaveURL(/sessions\/parent$/);
  expect(calls.filter((c) => c.action !== 'offer')).toHaveLength(0);
  await page.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
  await expect(text).toBeDisabled();
  await page.getByRole('button', { name: 'CANCEL', exact: true }).click();
  await page.evaluate(() => {
    location.hash = '/sessions/other';
  });
  await expect(text).toHaveValue('');
  await text.fill('Please continue');
  await page.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
  await page.getByRole('button', { name: 'CONFIRM SEND' }).click();
  await expect(
    page.getByRole('region', { name: 'Session controls' }),
  ).toContainText('Text sent.');
  expect(
    calls.filter((c) => c.action === 'prepare').at(-1)?.body,
  ).toMatchObject({ session: 'other', answers: ['Please continue'] });
  const storage = await page.evaluate(() =>
    JSON.stringify({ ...localStorage, ...sessionStorage }),
  );
  expect(storage).not.toContain('Synthetic private draft');
  expect(storage).not.toContain('Please continue');
});

test('structured questions and secret inputs fit narrow screens without executing text', async ({
  page,
  pairingURL,
}) => {
  const { offer, calls } = await mockActions(page, 'prompt');
  offer.questions = [
    {
      text: 'Choose environment',
      secret: false,
      freeText: false,
      options: ['Test', 'Production'],
    },
    { text: 'Secret answer', secret: true, freeText: true, options: [] },
  ];
  await page.setViewportSize({ width: 360, height: 700 });
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  await page.getByLabel('Choose environment').selectOption('Test');
  await page.getByLabel('Secret answer').fill('<img src=x onerror=alert(1)>');
  await expect(page.getByLabel('Secret answer')).toHaveAttribute(
    'type',
    'password',
  );
  await page.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
  await expect(
    page.getByRole('button', { name: 'CONFIRM SEND' }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await expect(page.locator('.session-actions img')).toHaveCount(0);
  expect(calls.filter((c) => c.action === 'prepare')[0].body.answers).toEqual([
    'Test',
    '<img src=x onerror=alert(1)>',
  ]);
  await page.screenshot({
    path: 'test-results/session-control.png',
    fullPage: true,
  });
});

test('secret fixed-choice answers remain masked through review and confirmation', async ({
  page,
  pairingURL,
}) => {
  const { offer, calls } = await mockActions(page, 'prompt');
  offer.questions = [
    {
      text: 'Private choice',
      secret: true,
      freeText: false,
      options: ['alpha-secret', 'beta-secret'],
    },
  ];
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const input = page.getByLabel('Private choice', { exact: true });
  const controls = page.getByRole('region', { name: 'Session controls' });
  await expect(input).toHaveAttribute('type', 'password');
  await expect(controls.locator('select')).toHaveCount(0);
  await expect(
    controls.getByText('alpha-secret', { exact: true }),
  ).not.toBeVisible();
  await input.fill('not-offered');
  await expect(
    controls.getByRole('button', { name: 'REVIEW BEFORE SENDING' }),
  ).toBeDisabled();
  await input.fill('alpha-secret');
  await controls.getByRole('button', { name: 'REVIEW BEFORE SENDING' }).click();
  await expect(input).toHaveAttribute('type', 'password');
  await expect(input).toBeDisabled();
  expect(await controls.innerText()).not.toContain('alpha-secret');
  await controls.getByRole('button', { name: 'CONFIRM SEND' }).click();
  await expect(controls).toContainText('Text sent.');
  expect(calls.filter((c) => c.action === 'prepare')[0].body.answers).toEqual([
    'alpha-secret',
  ]);
  expect(calls.filter((c) => c.action === 'commit')).toHaveLength(1);
});

test('main tabs match only declared route shapes', async ({
  page,
  pairingURL,
}) => {
  await page.goto(pairingURL);
  await expect(page.getByRole('meter').first()).toBeVisible();
  const nav = page.getByRole('navigation', { name: 'Main navigation' });
  for (const [path, tab] of [
    ['/', 'QUOTA'],
    ['/quota', 'QUOTA'],
    ['/quota/pie', 'QUOTA'],
    ['/sessions', 'SESSIONS'],
    ['/sessions/example', 'SESSIONS'],
    ['/usage', 'USAGE'],
    ['/usage/', 'USAGE'],
  ]) {
    await page.evaluate((path) => {
      location.hash = path;
    }, path);
    await expect(nav.locator('[aria-current="page"]')).toHaveText(tab);
    await expect(nav.locator('.active')).toHaveText(tab);
  }
  for (const path of [
    '/usage-old',
    '/sessionsx',
    '/quotafoo',
    '/usage/extra',
    '/quota/pie/extra',
    '/sessions/id/extra',
  ]) {
    await page.evaluate((path) => {
      location.hash = path;
    }, path);
    await expect(page.getByText('Page not found')).toBeVisible();
    await expect(nav.locator('[aria-current], .active')).toHaveCount(0);
  }
});

test('pairing, all quota views, navigation and refresh', async ({
  page,
  pairingURL,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto(pairingURL);
  await expect(
    page.getByRole('navigation', { name: 'Quota view' }),
  ).toBeVisible();
  await expect(page).toHaveURL(/#\/quota\/bars$/);
  await expect(page.getByRole('meter').first()).toBeVisible();
  await page.getByRole('link', { name: 'PIE', exact: true }).click();
  await expect(page.locator('main svg')).toHaveCount(2);
  await expect(
    page.getByRole('link', { name: 'ZONE', exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole('link', { name: 'PACE', exact: true }),
  ).toHaveCount(1);
  await page.getByRole('link', { name: 'PACE', exact: true }).click();
  await expect(page.locator('.consumption-zone')).toHaveCount(2);
  await expect(page.getByText('CONSUMPTION', { exact: true })).toHaveCount(2);
  await expect(
    page.getByText('DISTANCE FROM SAFETY (PP)', { exact: true }),
  ).toHaveCount(0);
  await expect(page.locator('.position-dot')).toHaveCount(2);
  await expect(page.locator('.pace-marker')).toHaveCount(0);
  await expect(page.getByRole('radio')).toHaveCount(0);
  await page.getByRole('link', { name: 'FUEL TANK', exact: true }).click();
  await expect(page.getByRole('meter', { name: 'Fuel remaining' })).toHaveCount(
    2,
  );
  await page.getByRole('link', { name: 'RESETS', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: /RESET INVENTORY/ }),
  ).toBeVisible();
  await expect(
    page.getByRole('button', { name: /CONFIRM|REDEEM/ }),
  ).toHaveCount(0);
  await page.goBack();
  await expect(page.getByRole('meter', { name: 'Fuel remaining' })).toHaveCount(
    2,
  );
  await page.reload();
  await expect(page.getByRole('meter', { name: 'Fuel remaining' })).toHaveCount(
    2,
  );
  await page.getByLabel('Theme', { exact: true }).selectOption('nightshade');
  await expect(page.locator('.shell')).toHaveAttribute(
    'data-theme',
    'nightshade',
  );
  await page.reload();
  await expect(page.locator('.shell')).toHaveAttribute(
    'data-theme',
    'nightshade',
  );
  await page.goto(pairingURL.split('#')[0] + '#/');
  await expect(
    page.getByRole('link', { name: 'QUOTA', exact: true }),
  ).toHaveAttribute('aria-current', 'page');
  await expect(
    page.locator('nav[aria-label="Main navigation"] [aria-current="page"]'),
  ).toHaveCount(1);
  expect(errors).toEqual([]);
});

test('history aggregates duplicate dates and excludes negative buckets in every view', async ({
  page,
  pairingURL,
}) => {
  await page.clock.setFixedTime(new Date('2026-09-11T12:00:00Z'));
  const snapshot = {
    version: 'test',
    meters: [],
    credits: [],
    creditCount: 0,
    sessions: [],
    quotaAt: '',
    sessionsAt: '',
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    usage: {
      summary: {
        longestRunningTurnSec: 45,
        longestStreakDays: 9,
      },
      dailyUsageBuckets: [
        { startDate: '2026-09-10', tokens: 100 },
        {
          startDate: '2026-09-10',
          tokens: 250,
        },
        { startDate: '2026-09-10', tokens: -50 },
        { startDate: '2026-09-11', tokens: 20 },
        { startDate: '2026-09-09', tokens: -500 },
        { startDate: 'not-a-date', tokens: 900 },
        { startDate: '2026-09-12', tokens: 800 },
      ],
    },
  };
  await page.route('**/api/events', (route) =>
    route.fulfill({
      contentType: 'text/event-stream',
      body: 'data: ' + JSON.stringify(snapshot) + '\n\n',
    }),
  );
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'USAGE', exact: true }).click();
  await expect(
    page.locator('.heat-cell[title^="2026-09-10: 350 tokens"]'),
  ).toHaveCount(1);
  await expect(
    page.locator('.heat-cell[title^="2026-09-09: 0 tokens"]'),
  ).toHaveCount(1);
  await expect(page.getByText('LONGEST TURN')).toBeVisible();
  await page.getByText('Accessible data table').click();
  await expect(
    page.getByRole('row').filter({
      has: page.getByRole('cell', { name: '2026-09-10', exact: true }),
    }),
  ).toContainText('350');
  await page
    .getByRole('navigation', { name: 'Usage view' })
    .getByRole('button', { name: 'Monthly', exact: true })
    .click();
  await expect(
    page.getByRole('row').filter({
      has: page.getByRole('cell', { name: '2026-09', exact: true }),
    }),
  ).toContainText('370');
  await page
    .getByRole('navigation', { name: 'Usage view' })
    .getByRole('button', { name: 'Cumulative', exact: true })
    .click();
  await expect(
    page.getByRole('row').filter({
      has: page.getByRole('cell', { name: '2026-09-11', exact: true }),
    }),
  ).toContainText('370');
});

test('usage breakdowns and allowance history preserve units, unknowns and read-only navigation', async ({
  page,
  pairingURL,
}) => {
  await mockStream(page, {
    meters: [],
    sessions: [],
    quotaAt: '',
    sessionsAt: '',
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    usage: {
      summary: {},
      dailyUsageBuckets: null,
      reports: {
        dailyStatus: 'OPENAI',
        planStatus: 'OPENAI',
        daily: {
          units: 'RELATIVE USAGE',
          from: '2026-09-04',
          through: '2026-10-03',
          fetchedAt: '2026-10-03T12:00:00Z',
          days: [
            {
              date: '2026-10-01',
              total: 2,
              groups: { model: { 'older-model': 2 } },
            },
            {
              date: '2026-10-02',
              total: 4,
              groups: {
                model: { 'gpt-6.1-sol': 2.5, 'gpt-6-luna': 1.5 },
                surface: { cli: 4 },
              },
            },
          ],
        },
        plan: {
          fetchedAt: '2026-10-03T12:00:00Z',
          data_as_of: '2026-10-03T11:00:00Z',
          coverage_start: '2026-09-26T00:00:00Z',
          coverage_complete: false,
          approximate: true,
          boundary_tolerance_seconds: 60,
          periods: [
            {
              id: 'p1',
              window_minutes: 10080,
              plan_type: 'pro',
              starts_at: '2026-09-28T00:00:00Z',
              ends_at: '2026-10-05T00:00:00Z',
              accounting_complete: false,
              used_basis_points: 12500,
              breakdowns: [
                {
                  dimension: 'model',
                  rows: [
                    { key: 'gpt-6.1-sol', basis_points: 12000 },
                    { key: 'unknown', basis_points: 500 },
                  ],
                },
              ],
            },
            {
              id: 'p2',
              window_minutes: 300,
              plan_type: 'pro',
              starts_at: '2026-09-27T00:00:00Z',
              ends_at: '2026-09-27T05:00:00Z',
              accounting_complete: false,
              used_basis_points: null,
              breakdowns: null,
            },
          ],
        },
      },
    },
  });
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'USAGE', exact: true }).click();
  await page
    .getByRole('navigation', { name: 'Usage view' })
    .getByRole('button', { name: 'Breakdown', exact: true })
    .click();
  await expect(
    page.getByRole('heading', { name: '2026-10-02 // 4 RELATIVE USAGE' }),
  ).toBeVisible();
  await page.getByLabel('Usage grouping').selectOption('model');
  await expect(page.locator('.report-row').first()).toContainText(
    'gpt-6.1-sol',
  );
  await expect(page.locator('.report-row').first()).toContainText('2.5');
  await expect(
    page.locator('.report-row').nth(1).locator('.report-fill'),
  ).toHaveCSS('opacity', '1');
  await expect(
    page.locator('.report-row').nth(1).locator('.report-fill'),
  ).toHaveCSS('background-image', /radial-gradient/);
  await expect(page.locator('.report-head')).toHaveText('RELATIVE USAGE');
  await page.getByRole('button', { name: '← OLDER', exact: true }).click();
  await expect(page.locator('.report-row')).toContainText('older-model');
  await page.getByRole('button', { name: 'NEWER →', exact: true }).click();
  await page
    .getByRole('navigation', { name: 'Usage view' })
    .getByRole('button', { name: 'Windows', exact: true })
    .click();
  await expect(page.getByText('OPENAI // UTC')).toBeVisible();
  await expect(
    page.getByRole('heading', { name: '10080 MIN // pro // USED 125%' }),
  ).toBeVisible();
  await expect(page.getByText(/Partial accounting/)).toBeVisible();
  await expect(page.locator('.quota-full')).toHaveText('USED 125%');
  await page.getByLabel('Usage grouping').selectOption('model');
  await expect(page.locator('.report-head')).toHaveText('QUOTA USED');
  await expect(page.locator('.report-value').first()).toHaveText('120%');
  await expect(page.locator('.report-row.unknown')).toContainText('unknown');
  await expect(page.locator('.report-row.unknown .report-fill')).toHaveCSS(
    'opacity',
    '1',
  );
  await page.getByRole('button', { name: '← OLDER', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: '300 MIN // pro // USED UNKNOWN' }),
  ).toBeVisible();
  await expect(page.locator('.quota-full, .quota-near')).toHaveCount(0);
  await expect(
    page.getByText(
      'Selected breakdown unavailable; missing does not mean zero.',
    ),
  ).toBeVisible();
  for (const width of [390, 1280]) {
    await page.setViewportSize({ width, height: 800 });
    await expect(
      page.getByRole('heading', { name: 'QUOTA WINDOWS', exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
  }
});

test('empty and all-zero graphs announce a zero peak without invalid heights', async ({
  page,
  pairingURL,
}) => {
  const snapshot = {
    version: 'test',
    meters: [],
    credits: [],
    creditCount: 0,
    usage: null,
    quotaAt: '',
    sessionsAt: '',
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    sessions: [[], [{ at: '2026-09-11T12:00:00Z', tokens: 0 }]].map(
      (samples, index) => ({
        id: String(index),
        directory: '/test',
        tokens: 0,
        agents: 0,
        status: 'IDLE',
        contextKind: 'LAST ACTIVITY',
        text: '',
        command: '',
        source: 'LOCAL',
        activity: '',
        samples,
      }),
    ),
  };
  await page.route('**/api/events', (route) =>
    route.fulfill({
      contentType: 'text/event-stream',
      body: 'data: ' + JSON.stringify(snapshot) + '\n\n',
    }),
  );
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  await expect(
    page.getByRole('img', {
      name: 'Token activity. Peak 0 tokens.',
      exact: true,
    }),
  ).toHaveCount(2);
  await expect(
    page.getByText('SCALE // 0 — 0 TOKENS', { exact: true }),
  ).toHaveCount(2);
  expect(
    await page
      .locator('.chart-bar')
      .evaluateAll((nodes) =>
        nodes.every((node) => (node as HTMLElement).style.height === '0%'),
      ),
  ).toBe(true);
});

test('pace graph plots bounded coordinates and handles missing windows', async ({
  page,
  pairingURL,
}) => {
  const now = new Date('2026-09-11T12:00:00Z');
  await page.clock.setFixedTime(now);
  const end = Math.floor(now.getTime() / 1000);
  const snapshot = {
    version: 'test',
    credits: [],
    creditCount: 0,
    sessions: [],
    usage: null,
    quotaAt: now.toISOString(),
    sessionsAt: '',
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    meters: [
      { name: 'Start', used: 0, duration: 100, reset: end + 7500, details: '' },
      {
        name: 'Fast consumption',
        used: 75,
        duration: 100,
        reset: end + 4500,
        details: '',
      },
      { name: 'End', used: 100, duration: 100, reset: end - 3000, details: '' },
      { name: 'Unknown', used: 40, duration: null, reset: null, details: '' },
    ],
  };
  await page.route('**/api/events', (route) =>
    route.fulfill({
      contentType: 'text/event-stream',
      body: 'data: ' + JSON.stringify(snapshot) + '\n\n',
    }),
  );
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'PACE', exact: true }).click();
  await expect(page.locator('.consumption-zone')).toHaveCount(3);
  expect(
    await page.locator('.position-dot').evaluateAll((nodes) =>
      nodes.map((node) => {
        const field = node.closest('svg')!.querySelector('.zone-field')!;
        const x = Number(field.getAttribute('x'));
        const y = Number(field.getAttribute('y'));
        const w = Number(field.getAttribute('width'));
        const h = Number(field.getAttribute('height'));
        return [
          Math.round((100 * (Number(node.getAttribute('cx')) - x)) / w),
          Math.round((100 * (y + h - Number(node.getAttribute('cy')))) / h),
        ];
      }),
    ),
  ).toEqual([
    [0, 0],
    [25, 75],
    [100, 100],
  ]);
  await expect(
    page.getByText('ABOVE THE LINE — CONSUMING FASTER THAN TIME'),
  ).toBeVisible();
  await expect(
    page.getByText('Cycle duration or reset date unavailable', {
      exact: false,
    }),
  ).toBeVisible();
  const ids = await page
    .locator('linearGradient')
    .evaluateAll((nodes) => nodes.map((node) => node.id));
  expect(new Set(ids).size).toBe(3);
  for (const width of [360, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
  await page.screenshot({
    path: 'test-results/consumption-zone.png',
    fullPage: true,
  });
});

test('Pace distinguishes empty, single and continuous observations', async ({
  page,
  pairingURL,
}) => {
  const now = new Date('2026-09-11T12:00:00Z');
  await page.clock.setFixedTime(now);
  const point = (minutesAgo: number, elapsed: number) => ({
    at: new Date(now.getTime() - minutesAgo * 60_000).toISOString(),
    elapsed,
    used: 25,
    break: false,
  });
  const snapshot = {
    version: 'test',
    credits: [],
    creditCount: 0,
    sessions: [],
    usage: null,
    quotaAt: now.toISOString(),
    sessionsAt: '',
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    meters: [
      {
        name: 'Weekly',
        used: 25,
        duration: 100,
        reset: Math.floor(now.getTime() / 1000) + 3000,
        details: '',
        trail: [] as ReturnType<typeof point>[],
      },
    ],
  };
  await mockStream(page, snapshot);
  await page.goto(pairingURL);
  for (const view of ['PACE']) {
    await page.getByRole('link', { name: view, exact: true }).click();
    for (const [trail, label] of [
      [[], 'NO OBSERVATIONS'],
      [[point(0, 50)], 'ONE OBSERVATION'],
      [[point(30, 20), point(0, 50)], '30 MIN CONTINUOUS'],
      [[], 'NO OBSERVATIONS'],
    ] as const) {
      snapshot.meters[0].trail = [...trail];
      await page.evaluate(
        (snapshot) =>
          window.dispatchEvent(
            new CustomEvent('test-snapshot', { detail: snapshot }),
          ),
        snapshot,
      );
      await expect(page.locator('.zone-controls small')).toHaveText(
        'HISTORY // ' + label,
      );
    }
  }
});

test('pace graph offers trace control and observed trend periods', async ({
  page,
  pairingURL,
}) => {
  const now = new Date('2026-09-11T12:00:00Z');
  await page.clock.setFixedTime(now);
  const end = Math.floor(now.getTime() / 1000);
  const point = (hoursAgo: number, elapsed: number, used: number) => ({
    at: new Date(now.getTime() - hoursAgo * 60 * 60 * 1000).toISOString(),
    elapsed,
    used,
    break: false,
  });
  const snapshot = {
    version: 'test',
    credits: [],
    creditCount: 0,
    sessions: [],
    usage: null,
    quotaAt: now.toISOString(),
    sessionsAt: '',
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    meters: [
      {
        name: 'Weekly',
        used: 40,
        duration: 7 * 24 * 60,
        reset: end + 3 * 24 * 60 * 60,
        details: '',
        trail: [
          point(25, 42, 20),
          point(24, 42.6, 21),
          point(1, 56.5, 38),
          point(0.25, 56.95, 39),
          point(0, 57.1, 40),
        ],
      },
    ],
  };
  await mockStream(page, snapshot);
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'PACE', exact: true }).click();

  const graph = page.locator('.consumption-zone');
  const trend = page.getByLabel('Trend period');
  const trace = page.getByRole('checkbox', { name: 'TRACE PATH' });
  await expect(trace).toBeChecked();
  await expect(page.locator('.observation-trail')).toBeVisible();
  await expect(page.locator('.observation-trail-casing')).toBeVisible();
  await expect(page.locator('.observation-details summary')).toBeVisible();
  await expect(page.locator('.trend-line')).toBeVisible();
  await expect(trend).toHaveValue('window');
  await expect(trend.locator('option[value="halfHour"]')).not.toHaveAttribute(
    'disabled',
  );
  await expect(trend.locator('option[value="hour"]')).not.toHaveAttribute(
    'disabled',
  );
  await expect(trend.locator('option[value="day"]')).not.toHaveAttribute(
    'disabled',
  );
  await expect(trend.locator('option[value="window"]')).not.toHaveAttribute(
    'disabled',
  );
  await trace.uncheck();
  await expect(page.locator('.observation-trail')).toHaveCount(0);
  await expect(page.locator('.observation-trail-casing')).toHaveCount(0);
  await expect(page.locator('.observation-details summary')).toBeVisible();
  await trace.check();
  await expect(page.locator('.observation-trail')).toBeVisible();
  await expect(page.locator('.observation-trail-casing')).toBeVisible();
  await expect(graph).toHaveAttribute('aria-label', /5 observations/);

  await trend.selectOption('off');
  await expect(page.locator('.trend-line')).toHaveCount(0);
  async function expectTrendStart(minutes: number) {
    const width = Number(
      await page.locator('.zone-field').getAttribute('width'),
    );
    const expectedX =
      48 + (((4 / 7) * 100 - (minutes / (7 * 24 * 60)) * 100) / 100) * width;
    const startX = Number(
      (await page.locator('.trend-line').getAttribute('d'))!.match(
        /^M([\d.]+)/,
      )![1],
    );
    expect(startX).toBeCloseTo(expectedX, 5);
  }
  await trend.selectOption('halfHour');
  await expect(page.locator('.trend-line')).toBeVisible();
  await expectTrendStart(30);
  await trend.selectOption('hour');
  await expect(page.locator('.trend-line')).toBeVisible();
  await expectTrendStart(60);
  await expect(page.locator('.trend-arrow')).toBeAttached();
  await expect(
    page.getByText(/PROJECTED AT RESET|EXHAUSTION PROJECTED/),
  ).toBeVisible();
  await trend.selectOption('day');
  await expect(page.locator('.trend-line')).toBeVisible();
  await expectTrendStart(24 * 60);
  await page.screenshot({
    path: 'test-results/consumption-zone-trend.png',
    fullPage: true,
  });

  // Window-average pace uses the known current dot, even with no history.
  snapshot.meters[0].trail = [];
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(trend).toHaveValue('window');
  await expect(trend.locator('option[value="halfHour"]')).toHaveAttribute(
    'disabled',
  );
  await trend.selectOption('window');
  const line = page.locator('.trend-line');
  await expect(line).toBeVisible();
  const axes = await page.locator('.axes').getAttribute('d');
  const bottom = /V([\d.]+)/.exec(axes!)![1];
  expect(await line.getAttribute('d')).toMatch(new RegExp(`^M48 ${bottom} L`));
  await expect(page.locator('.trend-summary')).toHaveText(
    'TREND // 70.0% PROJECTED AT RESET',
  );

  // No consumption yields a horizontal line; zero elapsed time has no rate.
  snapshot.meters[0].used = 0;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(page.locator('.trend-summary')).toHaveText(
    'TREND // 0.0% PROJECTED AT RESET',
  );
  expect(await line.getAttribute('d')).not.toMatch(/NaN|Infinity/);
  snapshot.meters[0].reset = end + 7 * 24 * 60 * 60;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(trend).toHaveValue('window');
  await expect(trend.locator('option[value="window"]')).toHaveAttribute(
    'disabled',
  );
  await expect(trend.locator('option[value="window"]')).toContainText(
    'NO TIME ELAPSED',
  );
  await expect(line).toHaveCount(0);
  // The default resumes automatically once the new window has elapsed time.
  snapshot.meters[0].used = 40;
  snapshot.meters[0].reset = end + 3 * 24 * 60 * 60;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(trend).toHaveValue('window');
  await expect(line).toBeVisible();
});

test('Pace preserves the former Zone coordinates, diagonal and endpoint colours', async ({
  page,
  pairingURL,
}) => {
  const now = new Date('2026-09-11T12:00:00Z');
  await page.clock.setFixedTime(now);
  const elapsed = (4 / 7) * 100;
  const meter = {
    name: 'Weekly',
    used: 80,
    duration: 7 * 24 * 60,
    reset: now.getTime() / 1000 + 3 * 24 * 60 * 60,
    details: '',
    trail: [{ at: now.toISOString(), elapsed, used: 80, break: false }],
  };
  const snapshot = {
    version: 'test',
    credits: [],
    creditCount: 0,
    sessions: [],
    usage: null,
    quotaAt: now.toISOString(),
    sessionsAt: '',
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    meters: [meter],
  };
  await mockStream(page, snapshot);
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'PACE', exact: true }).click();
  await expect(page).toHaveURL(/#\/quota\/pace$/);
  const dot = page.locator('.position-dot');
  const safe = page.locator('.pace-line');
  const line = page.locator('.trend-line');
  const trend = page.getByLabel('Trend period');
  const bottom = Number(
    /V([\d.]+)/.exec((await page.locator('.axes').getAttribute('d'))!)![1],
  );
  const plotWidth = Number(
    await page.locator('.zone-field').getAttribute('width'),
  );
  const expectedX = 48 + (elapsed / 100) * plotWidth;
  const expectedY = bottom - 0.8 * (bottom - 20);
  expect(Number(await dot.getAttribute('cx'))).toBeCloseTo(expectedX, 5);
  expect(Number(await dot.getAttribute('cy'))).toBeCloseTo(expectedY, 5);
  expect(Number(await safe.getAttribute('y1'))).toBe(bottom);
  expect(Number(await safe.getAttribute('y2'))).toBe(20);
  await expect(page.locator('.axis-title').first()).toHaveText('CONSUMPTION');
  await expect(page.locator('linearGradient')).toHaveAttribute('x2', '100%');
  expect(await page.locator('.observation-trail').getAttribute('d')).toBe(
    `M${expectedX} ${expectedY}`,
  );
  await expect(line).toHaveCSS('stroke', 'rgb(127, 24, 37)');
  async function update() {
    await page.evaluate(
      (detail) =>
        window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
      snapshot,
    );
  }
  function recentSlope(slope: number) {
    const delta = (0.5 / (7 * 24)) * 100;
    meter.trail = [
      {
        at: new Date(now.getTime() - 30 * 60_000).toISOString(),
        elapsed: elapsed - delta,
        used: 80 - slope * delta,
        break: false,
      },
      { at: now.toISOString(), elapsed, used: 80, break: false },
    ];
  }
  // Even a slope below the diagonal is unsafe if it exhausts quota before reset.
  recentSlope(0.8);
  await update();
  await trend.selectOption('halfHour');
  await expect(page.locator('.trend-summary')).toHaveText(
    'TREND // QUOTA EXHAUSTION PROJECTED',
  );
  await expect(line).toHaveCSS('stroke', 'rgb(127, 24, 37)');
  const startX = 48 + ((elapsed - (0.5 / (7 * 24)) * 100) / 100) * plotWidth;
  let coords = (await line.getAttribute('d'))!.match(/[\d.-]+/g)!.map(Number);
  expect(coords[0]).toBeCloseTo(startX, 5);
  expect(coords.at(-2)!).toBeLessThan(48 + plotWidth);
  expect(coords.at(-1)!).toBeCloseTo(20, 5);
  // Safe projection reaches the right edge below 100% consumption.
  recentSlope(0.1);
  await update();
  await expect(line).toHaveCSS('stroke', 'rgb(17, 91, 53)');
  await expect(page.locator('.trend-summary')).toHaveText(
    'TREND // 84.3% PROJECTED AT RESET',
  );
  coords = (await line.getAttribute('d'))!.match(/[\d.-]+/g)!.map(Number);
  expect(coords.at(-2)!).toBeCloseTo(48 + plotWidth, 5);
  expect(coords.at(-1)!).toBeGreaterThan(20);
  // Exactly 100% projected consumption still uses the safe endpoint colour.
  meter.used = elapsed;
  await update();
  await trend.selectOption('window');
  await expect(line).toHaveCSS('stroke', 'rgb(17, 91, 53)');
  expect(await line.getAttribute('d')).not.toMatch(/NaN|Infinity/);
});

test('old graph bookmarks and saved selections restore the renamed Pace view', async ({
  page,
  pairingURL,
}) => {
  await page.addInitScript(() =>
    localStorage.setItem(
      'codexometer.web.preferences.v1',
      JSON.stringify({ tab: 'quota', view: 'zone' }),
    ),
  );
  await page.goto(pairingURL);
  await expect(page).toHaveURL(/#\/quota\/pace$/);
  for (const alias of ['zone', 'consumption-pace', 'pace']) {
    await page.evaluate((alias) => {
      location.hash = '/quota/' + alias;
    }, alias);
    await expect(page).toHaveURL(/#\/quota\/pace$/);
    await expect(
      page.getByRole('link', { name: 'PACE', exact: true }),
    ).toHaveAttribute('aria-current', 'page');
    await expect(
      page.getByRole('link', { name: 'ZONE', exact: true }),
    ).toHaveCount(0);
    await expect(page.locator('.axis-title').first()).toHaveText('CONSUMPTION');
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            JSON.parse(localStorage.getItem('codexometer.web.preferences.v1')!)
              .view,
        ),
      )
      .toBe('pace');
    await page.reload();
    await expect(
      page.getByRole('link', { name: 'PACE', exact: true }),
    ).toHaveAttribute('aria-current', 'page');
  }
});

test('quota graphics use viewport height and keep compact navigation accessible', async ({
  page,
  pairingURL,
}) => {
  await page.setViewportSize({ width: 1280, height: 700 });
  await page.goto(pairingURL);
  for (const [name, graphic] of [
    ['BARS', '.gauge:not(.timeline)'],
    ['PACE', '.zone-canvas'],
    ['PIE', '.pie-wrap'],
    ['FUEL TANK', '.gauge:not(.timeline)'],
  ]) {
    await page.setViewportSize({ width: 1280, height: 700 });
    await page.getByRole('link', { name, exact: true }).click();
    const plot = page.locator(graphic).first();
    await expect(plot).toBeVisible();
    const small = (await plot.boundingBox())!.height;
    await page.setViewportSize({ width: 1280, height: 1100 });
    await expect
      .poll(async () => (await plot.boundingBox())!.height)
      .toBeGreaterThan(small + 80);
    expect(
      await page
        .locator('main')
        .evaluate((el) => el.scrollHeight <= el.clientHeight + 2),
    ).toBe(true);
    await page.setViewportSize({ width: 360, height: 400 });
    await expect(page.getByLabel('Theme', { exact: true })).toBeInViewport();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await plot.scrollIntoViewIfNeeded();
    await expect(plot).toBeVisible();
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByRole('link', { name: 'PACE', exact: true }).click();
  await expect(page.locator('.zone-canvas')).toHaveCount(2);
  await page.screenshot({ path: 'test-results/responsive-quota.png' });
});

test('session detail deep links and responsive layout', async ({
  page,
  pairingURL,
}) => {
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  await expect(page.locator('.session-row')).toHaveCount(2);
  await page.getByRole('button', { name: 'SHOW DETAIL' }).first().click();
  await expect(page.locator('.context')).toContainText('Allow pushing');
  await page.getByRole('link', { name: 'FULL DETAIL →' }).first().click();
  await expect(page.locator('.full-detail')).toContainText(
    'git push origin main',
  );
  await page.reload();
  await expect(page.locator('.full-detail')).toContainText(
    'git push origin main',
  );
  await page.getByRole('link', { name: '← ALL SESSIONS' }).click();
  await expect(page.locator('.session-row').first()).toHaveClass(/wide/);
  for (const width of [360, 768, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await expect(
      page.getByRole('link', { name: 'FULL DETAIL →' }).first(),
    ).toBeVisible();
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.screenshot({ path: 'test-results/sessions.png', fullPage: true });
  await page.goto(pairingURL.split('#')[0] + '#/sessions/missing');
  await expect(
    page.getByText('This session is no longer', { exact: false }),
  ).toBeVisible();
});

test('per-session detail navigation, selection and preferences survive reload', async ({
  page,
  pairingURL,
}) => {
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'PIE', exact: true }).click();
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  const rows = page.locator('.session-row');
  await expect(rows).toHaveCount(2);
  await page.keyboard.press('ArrowDown');
  await expect(rows.nth(1)).toHaveClass(/selected/);
  await page.keyboard.press('ArrowRight');
  await expect(rows.nth(1).locator('.context')).toHaveCount(1);
  await expect(rows.first().locator('.context')).toHaveCount(0);
  await rows.nth(1).getByRole('button', { name: 'More detail' }).click();
  await expect(rows.nth(1).locator('.graph-panel')).toHaveCount(0);
  const widths = await rows
    .locator('.telemetry')
    .evaluateAll((elements) =>
      elements.map((element) => element.getBoundingClientRect().width),
    );
  expect(Math.abs(widths[0] - widths[1])).toBeLessThan(1);
  await page.keyboard.press('ArrowRight');
  await expect(page.locator('.full-detail')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(rows.nth(1)).toHaveClass(/wide/);
  await page.reload();
  await expect(rows.nth(1)).toHaveClass(/selected/);
  await expect(rows.nth(1)).toHaveClass(/wide/);
  await page.keyboard.press('ArrowLeft');
  await expect(rows.nth(1).locator('.graph-panel')).toHaveCount(1);
  await page.keyboard.press('ArrowLeft');
  await expect(rows.nth(1).locator('.context')).toHaveCount(0);
  await page
    .getByRole('button', { name: 'SHOW ALL DETAILS', exact: true })
    .click();
  await expect(page.locator('.context')).toHaveCount(2);
  await page
    .getByRole('button', { name: 'HIDE ALL DETAILS', exact: true })
    .click();
  await expect(page.locator('.context')).toHaveCount(0);
  await page.getByRole('link', { name: 'QUOTA', exact: true }).click();
  await expect(page).toHaveURL(/#\/quota\/pie$/);
  await page.getByRole('link', { name: 'CODEXOMETER', exact: true }).click();
  await expect(page).toHaveURL(/#\/quota\/bars$/);
});

test('all full-detail entry routes return to a selected wide row with browser Back', async ({
  page,
  pairingURL,
}) => {
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  const row = page.locator('.session-row').first();
  for (const entry of ['attention', 'full link', 'badge', 'arrows']) {
    await page
      .getByRole('button', { name: 'HIDE ALL DETAILS', exact: true })
      .click();
    if (entry === 'attention')
      await page
        .getByRole('navigation', { name: 'Sessions needing attention' })
        .getByRole('link')
        .first()
        .click();
    else if (entry === 'full link')
      await row.getByRole('link', { name: 'FULL DETAIL →' }).click();
    else if (entry === 'badge') {
      await row
        .getByRole('button', { name: 'SHOW DETAIL', exact: true })
        .click();
      await row.locator('.attention-badge').click();
    } else {
      for (let step = 0; step < 3; step++)
        await row.getByRole('button', { name: 'More detail' }).click();
    }
    await expect(page.locator('.full-detail')).toBeVisible();
    await page.goBack();
    await expect(row).toHaveClass(/wide/);
    await expect(row).toHaveClass(/selected/);
    await expect(row.locator('.graph-panel')).toHaveCount(0);
  }
});

test('saved landing tab restores on pairing and invalid preferences are ignored', async ({
  page,
  pairingURL,
}) => {
  await page.addInitScript(() => {
    localStorage.setItem(
      'codexometer.web.preferences.v1',
      JSON.stringify({
        tab: 'sessions',
        view: 'pie',
        layouts: [null, { id: 'bad', level: 99 }],
      }),
    );
  });
  await page.goto(pairingURL);
  await expect(page).toHaveURL(/#\/sessions$/);
  await expect(page.locator('.session-row')).toHaveCount(2);
  await expect(page.locator('.context')).toHaveCount(0);
});

test('blocked preference storage keeps navigation functional', async ({
  page,
  pairingURL,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(window, 'localStorage', {
      get() {
        throw new Error('blocked');
      },
    });
  });
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  await page.getByRole('button', { name: 'More detail' }).first().click();
  await expect(page.locator('.context')).toHaveCount(1);
});

test('attention distinguishes observed signals, inference and stale state without actions', async ({
  page,
  pairingURL,
}) => {
  const snapshot = {
    version: 'test',
    meters: [],
    credits: [],
    creditCount: 0,
    usage: null,
    quotaAt: '',
    sessionsAt: '',
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    sessions: [
      'APPROVAL NEEDED',
      'CHECK SESSION',
      'INPUT NEEDED',
      'TURN COMPLETE',
    ].map((status, index) => ({
      id: String(index),
      directory: '/test/' + index,
      tokens: 0,
      agents: 0,
      status,
      contextKind: 'LAST REPLY',
      text: 'A lengthy explanation. '.repeat(100),
      command: '',
      source: 'LOCAL',
      activity: '',
      samples: [],
    })),
  };
  await mockStream(page, snapshot);
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  await expect(
    page
      .getByRole('navigation', { name: 'Sessions needing attention' })
      .getByRole('link'),
  ).toHaveCount(3);
  await page
    .getByRole('button', { name: 'SHOW ALL DETAILS', exact: true })
    .click();
  const rows = page.locator('.session-row');
  await expect(rows.nth(0).locator('.attention-badge')).toBeVisible();
  await expect(rows.nth(0).locator('.telemetry')).toContainText(
    'APPROVE OR DECLINE IN CODEX',
  );
  await expect(rows.nth(2).locator('.telemetry')).toContainText(
    'REPLY IN CODEX',
  );
  await expect(rows.nth(0)).toContainText(
    'Command unavailable from this observation',
  );
  await expect(rows.nth(1)).toContainText(
    'not a confirmed input or approval request',
  );
  await expect(rows.nth(2)).toContainText('OBSERVED INPUT SIGNAL');
  await expect(rows.nth(3)).toContainText(
    'informational, not an approval request',
  );
  snapshot.sessions[0].command = 'git status';
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(rows.nth(0).locator('.command')).toHaveText('git status');
  await page.setViewportSize({ width: 360, height: 700 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await rows.nth(0).locator('.attention-badge').click();
  await expect(page.locator('.full-detail .command')).toHaveText('git status');
  await expect(
    page.getByRole('button', { name: /APPROVE|DECLINE|SEND|CONFIRM/ }),
  ).toHaveCount(0);
  snapshot.sessionsError = true;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(page.locator('.full-detail h2')).toContainText('STALE');
  await expect(
    page.getByRole('navigation', { name: 'Sessions needing attention' }),
  ).toHaveCount(0);
  await expect(page.locator('.full-detail')).toContainText(
    'LAST OBSERVED COMMAND',
  );
  snapshot.sessionsError = false;
  snapshot.sessions[0].status = 'WORKING';
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(page.locator('.full-detail h2')).toContainText('WORKING');
  await page.keyboard.press('Escape');
  await expect(rows.first().locator('.attention-badge')).toHaveCount(0);
  await rows.nth(1).locator('.session-select').click();
  snapshot.sessions.splice(1, 1);
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(rows.first()).toHaveClass(/selected/);
});

test('global detail controls cover more than 100 sessions and survive reload', async ({
  page,
  pairingURL,
}) => {
  const snapshot = {
    version: 'test',
    meters: [],
    credits: [],
    creditCount: 0,
    usage: null,
    quotaAt: '',
    sessionsAt: '',
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    sessions: Array.from({ length: 105 }, (_, index) => ({
      id: String(index),
      directory: '/test/' + index,
      tokens: 0,
      agents: 0,
      status: 'IDLE',
      contextKind: 'LAST ACTIVITY',
      text: '',
      command: '',
      source: 'LOCAL',
      activity: '',
      samples: [],
    })),
  };
  await mockStream(page, snapshot);
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  await page
    .getByRole('button', { name: 'SHOW ALL DETAILS', exact: true })
    .click();
  await expect(page.locator('.context')).toHaveCount(105);
  // An explicit zero must override the nonzero global default.
  await page
    .getByRole('button', { name: 'HIDE DETAIL', exact: true })
    .first()
    .click();
  await expect(
    page.locator('.session-row').first().locator('.context'),
  ).toHaveCount(0);
  await page.reload();
  await expect(page.locator('.context')).toHaveCount(104);
  await page
    .getByRole('button', { name: 'HIDE ALL DETAILS', exact: true })
    .click();
  await expect(page.locator('.context')).toHaveCount(0);
  await page.reload();
  await expect(page.locator('.session-row')).toHaveCount(105);
  await expect(page.locator('.context')).toHaveCount(0);
  await page
    .getByRole('button', { name: 'SHOW ALL DETAILS', exact: true })
    .click();
  await expect(page.locator('.context')).toHaveCount(105);
});

test('session totals count parent usage once and update for live, stale and empty lists', async ({
  page,
  pairingURL,
}) => {
  const snapshot = {
    version: 'test',
    meters: [],
    credits: [],
    creditCount: 0,
    usage: null,
    quotaAt: '',
    sessionsAt: '',
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    sessions: [
      'WORKING',
      'WORKING',
      'APPROVAL NEEDED',
      'INPUT NEEDED',
      'CHECK SESSION',
      'TURN COMPLETE',
    ].map((status, index) => ({
      id: String(index),
      directory: '/session/' + index,
      tokens: 1000,
      agents: 3,
      status,
      contextKind: 'LAST REPLY',
      text: '',
      command: '',
      source: 'LOCAL',
      activity: '',
      samples: [{ at: '', tokens: 500 }],
    })),
  };
  await mockStream(page, snapshot);
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  const total = (label: string) =>
    page
      .locator('.session-totals > div')
      .filter({ has: page.getByText(label, { exact: true }) })
      .locator('dd');
  for (const [label, value] of [
    ['OBSERVED TOKENS', '6,000'],
    ['LISTED SESSIONS', '6'],
    ['WORKING', '2'],
    ['AWAITING APPROVAL', '1'],
    ['AWAITING INPUT', '1'],
    ['CHECK · INFERRED', '1'],
  ])
    await expect(total(label)).toHaveText(value);
  // Changing detail never narrows the aggregate to the selected session.
  await page.getByRole('link', { name: 'FULL DETAIL →' }).first().click();
  await expect(total('OBSERVED TOKENS')).toHaveText('6,000');
  snapshot.sessions[0].tokens += 250;
  snapshot.sessions.splice(1, 1);
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(total('OBSERVED TOKENS')).toHaveText('5,250');
  await expect(total('LISTED SESSIONS')).toHaveText('5');
  await expect(total('WORKING')).toHaveText('1');
  snapshot.sessionsError = true;
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(total('OBSERVED TOKENS')).toHaveText('5,250');
  await expect(page.getByText(/LAST KNOWN TOTALS/)).toBeVisible();
  for (const label of [
    'WORKING',
    'AWAITING APPROVAL',
    'AWAITING INPUT',
    'CHECK · INFERRED',
  ])
    await expect(total(label)).toHaveText('—');
  snapshot.sessionsError = false;
  snapshot.sessions = [];
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(page.locator('.session-totals dd')).toHaveText([
    '0',
    '0',
    '0',
    '0',
    '0',
    '0',
  ]);
  await page.setViewportSize({ width: 360, height: 600 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test('pace trail renders start, gap and live updates across navigation and reload', async ({
  page,
  pairingURL,
}) => {
  const snapshot = {
    version: 'test',
    sessions: [],
    credits: [],
    creditCount: 0,
    usage: null,
    quotaAt: '',
    sessionsAt: '',
    usageAt: '',
    quotaError: false,
    sessionsError: false,
    usageError: false,
    meters: [
      {
        name: 'Weekly',
        used: 40,
        duration: 10080,
        reset: Math.floor(Date.now() / 1000) + 3600,
        details: '',
        trail: [
          { at: '2026-09-10T12:00:00Z', elapsed: 10, used: 5, break: false },
          { at: '2026-09-10T13:00:00Z', elapsed: 20, used: 15, break: false },
          { at: '2026-09-10T15:00:00Z', elapsed: 40, used: 40, break: true },
        ],
      },
    ],
  };
  await mockStream(page, snapshot);
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'PACE', exact: true }).click();
  const trail = page.locator('.observation-trail');
  const trace = page.getByRole('checkbox', { name: 'TRACE PATH' });
  await expect(trace).toBeChecked();
  await expect(page.locator('.trail-start')).toHaveCount(1);
  expect((await trail.getAttribute('d'))?.match(/M/g)).toHaveLength(2);
  expect((await trail.getAttribute('d'))?.match(/L/g)).toHaveLength(1);
  const summary = page.locator('.observation-details summary');
  await summary.focus();
  await page.keyboard.press('Enter');
  const table = page.getByRole('table', { name: 'Quota observations' });
  await expect(table).toBeVisible();
  const dataRows = table.locator('tbody tr');
  await expect(dataRows).toHaveCount(3);
  await expect(dataRows.nth(0)).toContainText('10.0%');
  await expect(dataRows.nth(0)).toContainText('5%');
  await expect(dataRows.nth(0)).toContainText('First observation');
  await expect(dataRows.nth(0).locator('time')).toHaveAttribute(
    'datetime',
    '2026-09-10T12:00:00Z',
  );
  await expect(dataRows.nth(1)).toContainText(
    'Connected to previous observation',
  );
  await expect(dataRows.nth(2)).toContainText('Gap before this observation');
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  await page.getByRole('link', { name: 'QUOTA', exact: true }).click();
  await expect(trace).toBeChecked();
  await expect(trail).toHaveCount(1);
  await page.reload();
  await expect(trace).toBeChecked();
  await expect(trail).toHaveCount(1);
  await expect(page.locator('.consumption-zone')).toHaveAttribute(
    'aria-label',
    /3 observations/,
  );
  await page.locator('.observation-details summary').click();
  await expect(table).toBeVisible();
  snapshot.meters[0].trail.push({
    at: '2026-09-10T16:00:00Z',
    elapsed: 50,
    used: 45,
    break: false,
  });
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(page.locator('.consumption-zone')).toHaveAttribute(
    'aria-label',
    /4 observations/,
  );
  await expect(table.locator('tbody tr')).toHaveCount(4);
  snapshot.meters[0].trail = [snapshot.meters[0].trail[3]];
  await page.evaluate(
    (detail) =>
      window.dispatchEvent(new CustomEvent('test-snapshot', { detail })),
    snapshot,
  );
  await expect(page.locator('.consumption-zone')).toHaveAttribute(
    'aria-label',
    /1 observation;/,
  );
  await expect(table.locator('tbody tr')).toHaveCount(1);
});

test('usage heatmap, period selection, bars and accessible table', async ({
  page,
  pairingURL,
}) => {
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'USAGE', exact: true }).click();
  await expect
    .poll(() => page.locator('.heat-cell').count())
    .toBeGreaterThanOrEqual(365);
  expect(await page.locator('.heat-cell').count()).toBeLessThanOrEqual(366);
  await page.getByLabel('Usage period').selectOption('6');
  expect(await page.locator('.heat-cell').count()).toBeGreaterThan(175);
  expect(await page.locator('.heat-cell').count()).toBeLessThan(186);
  await page
    .getByRole('navigation', { name: 'Usage view' })
    .getByRole('button', { name: 'Monthly', exact: true })
    .click();
  await expect(page.locator('.chart')).toBeVisible();
  await page
    .getByRole('navigation', { name: 'Usage view' })
    .getByRole('button', { name: 'Cumulative', exact: true })
    .click();
  await page.getByText('Accessible data table').click();
  await expect(page.getByRole('table')).toBeVisible();
  await page.getByRole('button', { name: '← EARLIER' }).click();
  await expect(page.getByRole('button', { name: 'LATER →' })).toBeEnabled();
  await page.getByRole('button', { name: 'LATER →' }).click();
  await expect(page.getByRole('button', { name: 'LATER →' })).toBeDisabled();
});

test('unauthenticated tabs cannot read data and pairing is one-use', async ({
  browser,
  page,
  pairingURL,
}) => {
  const origin = new URL(pairingURL).origin;
  const response = await page.request.get(origin + '/api/state');
  expect(response.status()).toBe(401);
  expect(response.headers()['content-security-policy']).toContain(
    "frame-ancestors 'none'",
  );
  await page.goto(pairingURL);
  await expect(page.getByRole('meter').first()).toBeVisible();
  const other = await browser.newContext();
  try {
    const tab = await other.newPage();
    await tab.goto(pairingURL);
    await expect(tab.getByRole('status')).toContainText(
      'expired or already used',
    );
    await expect(tab.getByRole('meter')).toHaveCount(0);
  } finally {
    await other.close();
  }
});

test('session content is text, not HTML or executable instructions', async ({
  page,
  pairingURL,
}) => {
  const dialogs: string[] = [];
  const externalRequests: string[] = [];
  page.on('dialog', async (dialog) => {
    dialogs.push(dialog.message());
    await dialog.dismiss();
  });
  page.on('request', (request) => {
    if (request.url().includes('evil.example'))
      externalRequests.push(request.url());
  });
  const snapshot = {
    version: 'test',
    meters: [],
    credits: [],
    creditCount: 0,
    usage: null,
    quotaAt: '',
    usageAt: '',
    sessionsAt: '',
    quotaError: false,
    usageError: false,
    sessionsError: false,
    sessions: [
      {
        id: 'untrusted" onclick="alert(1)',
        directory: '<img src="https://evil.example" onerror="alert(1)">',
        tokens: 100,
        agents: 0,
        status: 'INPUT NEEDED',
        contextKind: '<svg onload="alert(1)"></svg>',
        text: '<img src="https://evil.example" onerror="alert(1)"><script>alert(1)</script>',
        command: '<iframe src="https://evil.example"></iframe>',
        source: 'LOCAL',
        samples: [],
        activity: '',
      },
    ],
  };
  await page.route('**/api/events', (route) =>
    route.fulfill({
      contentType: 'text/event-stream',
      body: 'data: ' + JSON.stringify(snapshot) + '\n\n',
    }),
  );
  await page.goto(pairingURL);
  await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
  await expect(page.locator('.session-select')).toHaveText(
    snapshot.sessions[0].directory,
  );
  await page.getByRole('link', { name: 'FULL DETAIL →' }).click();
  await expect(page.locator('.full-detail pre').first()).toContainText('<img');
  await expect(
    page.locator(
      '.full-detail img, .full-detail script, .full-detail iframe, .full-detail svg',
    ),
  ).toHaveCount(0);
  await expect(
    page.getByRole('button', { name: /APPROVE|CONFIRM|SEND/ }),
  ).toHaveCount(0);
  expect(dialogs).toEqual([]);
  expect(externalRequests).toEqual([]);
});

test('cwd shows the running directory without preparing a mutation', async ({
  page,
  pairingURL,
}) => {
  await mockActions(page, 'prompt');
  const modes: string[] = [];
  await page.route('**/api/control/commands', async (route) => {
    const { mode, path } = route.request().postDataJSON().command;
    modes.push(mode);
    await route.fulfill({
      json:
        path === ''
          ? {
              title: '/ COMMANDS',
              help: 'Commands',
              path: '',
              revision: 'r',
              choices: [
                {
                  id: 'cwd',
                  label: '/cwd',
                  help: 'Show directory',
                  next: 'cwd',
                },
              ],
            }
          : {
              title: '/cwd',
              help: 'Running directory',
              path: 'cwd',
              revision: 'r',
              choices: [
                {
                  id: 'directory',
                  label: 'Current directory',
                  help: 'Working directory: /dev/projects',
                },
              ],
            },
    });
  });
  await page.goto(pairingURL);
  await page.evaluate(() => {
    location.hash = '/sessions/parent';
  });
  const panel = page.getByRole('region', { name: 'Session slash commands' });
  await panel.getByRole('button', { name: '/ COMMANDS', exact: true }).click();
  await panel.getByRole('button', { name: '/cwd →', exact: true }).click();
  await panel
    .getByRole('button', { name: 'Current directory // HELP', exact: true })
    .click();
  await expect(
    panel.getByText('Working directory: /dev/projects', { exact: true }),
  ).toBeVisible();
  await expect(
    panel.getByRole('button', { name: 'CONFIRM CHANGE' }),
  ).toHaveCount(0);
  expect(modes.every((mode) => mode === 'list')).toBe(true);
});

test.describe('browser startup', () => {
  test.use({
    reducedMotion: 'no-preference',
    controlMode: true,
    deviceScaleFactor: 2,
  });

  async function freezeStartup(page: Page, choice: number) {
    await page.addInitScript((value) => {
      Math.random = () => value;
    }, choice);
    const time = new Date('2026-10-10T12:00:00Z');
    await page.clock.install({ time });
    await page.clock.pauseAt(new Date(time.getTime() + 1000));
  }

  // Assert parity against the terminal source, rather than a second browser font.
  const terminalRows = [
    ...readFileSync(
      resolve('../internal/ui/header_actions.go'),
      'utf8',
    ).matchAll(/"([█▀▄ ][█▀▄ ]+)"/g),
  ]
    .slice(0, 2)
    .map((match) => [...match[1]]);
  const expectedPixels: string[] = [];
  terminalRows.forEach((line, row) =>
    line.forEach((cell, col) => {
      if (cell === '█' || cell === '▀')
        expectedPixels.push(`${col},${row * 2}`);
      if (cell === '█' || cell === '▄')
        expectedPixels.push(`${col},${row * 2 + 1}`);
    }),
  );

  for (const [variant, choice, duration] of [
    ['slide', 0.1, 900],
    ['typing', 0.4, 2070],
    ['shuffle', 0.8, 1210],
  ] as const) {
    test(`${variant} uses the terminal font and docks while pairing loads`, async ({
      page,
      pairingURL,
    }) => {
      const errors: string[] = [];
      page.on('pageerror', (error) => errors.push(error.message));
      await freezeStartup(page, choice);
      await page.goto(pairingURL);
      const intro = page.locator('.startup');
      const wordmark = intro.locator('.wordmark');
      await expect(intro).toHaveAttribute('data-entrance', variant);
      await page.clock.runFor(32);
      const large = (await wordmark.boundingBox())!;
      expect(large.width).toBeCloseTo(1280, 0);
      expect(large.y + large.height / 2).toBeCloseTo(360, 0);
      if (variant === 'slide') expect(large.x).toBeGreaterThan(0);
      else expect(large.x).toBeCloseTo(0, 0);
      const initial = (await wordmark.getAttribute('data-text'))!;
      expect(initial.length).toBe(11);
      if (variant === 'typing') expect(initial.trim()).toBe('');
      if (variant === 'shuffle') {
        expect(initial).toMatch(/^[A-Z0-9]{11}$/);
        [...initial].forEach((letter, index) =>
          expect(letter).not.toBe('CODEXOMETER'[index]),
        );
      }
      await expect(page.locator('.connection .lamp')).toHaveClass(/lit/);
      await expect(page.locator('footer')).toContainText(
        'Session control enabled',
      );
      await expect(page.locator('header')).toHaveAttribute('inert', '');
      await expect(page.locator('main')).toBeHidden();
      await page.clock.runFor(duration + 30 - 32);
      await expect(wordmark).toHaveAttribute('data-text', 'CODEXOMETER');
      const path = await wordmark.locator('path').getAttribute('d');
      expect(
        [...path!.matchAll(/M(\d+) (\d+)/g)]
          .map((match) => `${match[1]},${match[2]}`)
          .sort(),
      ).toEqual([...expectedPixels].sort());
      expect(await wordmark.locator('path').getAttribute('d')).toBe(
        await page.locator('.brand path').getAttribute('d'),
      );
      const full = (await wordmark.boundingBox())!;
      expect(full.x).toBeCloseTo(0, 0);
      expect(full.width).toBeCloseTo(large.width, 0);
      // The SVG must paint at its displayed size, rather than magnifying a
      // small compositor raster. This also exercises Retina-scale rendering.
      const paintWidth = await wordmark
        .locator('svg')
        .evaluate((svg) => svg.clientWidth);
      expect(paintWidth).toBeCloseTo(full.width, 0);
      await page.clock.runFor(949);
      const near = (await wordmark.boundingBox())!;
      const target = (await page.locator('.brand').boundingBox())!;
      // The last animation frame may be up to 16 ms before its exact endpoint.
      expect(Math.abs(near.x - target.x)).toBeLessThan(2);
      expect(Math.abs(near.y - target.y)).toBeLessThan(2);
      expect(Math.abs(near.width - target.width)).toBeLessThan(2);
      await page.clock.runFor(100);
      await expect(intro).toHaveCount(0);
      await expect(
        page.getByRole('link', { name: 'CODEXOMETER', exact: true }),
      ).toBeVisible();
      await expect(page.locator('header')).not.toHaveAttribute('inert', '');
      await page.getByRole('link', { name: 'SESSIONS', exact: true }).click();
      await expect(page.locator('.session-row')).toHaveCount(2);
      await page
        .getByRole('link', { name: 'CODEXOMETER', exact: true })
        .click();
      await expect(page).toHaveURL(/#\/quota\/bars$/);
      await page.clock.runFor(4000);
      await expect(intro).toHaveCount(0);
      await page.reload();
      await expect(intro).toHaveAttribute('data-entrance', variant);
      expect(errors).toEqual([]);
    });
  }

  test('typing blinks three times and advances its dot through the fixed slots', async ({
    page,
    pairingURL,
  }) => {
    await freezeStartup(page, 0.4);
    await page.goto(pairingURL);
    const wordmark = page.locator('.startup .wordmark');
    let elapsed = 0;
    for (const at of [16, 216, 376, 576, 736, 936]) {
      await page.clock.runFor(at - elapsed);
      elapsed = at;
      await expect(wordmark).toHaveAttribute('data-text', ' '.repeat(11));
      if ([16, 376, 736].includes(at))
        await expect(wordmark).toHaveAttribute('data-cursor', '0');
      else await expect(wordmark).not.toHaveAttribute('data-cursor');
    }
    for (let count = 1; count <= 11; count++) {
      const at = 1096 + (count - 1) * 90;
      await page.clock.runFor(at - elapsed);
      elapsed = at;
      await expect(wordmark).toHaveAttribute(
        'data-text',
        'CODEXOMETER'.slice(0, count).padEnd(11),
      );
      if (count < 11)
        await expect(wordmark).toHaveAttribute('data-cursor', String(count));
      else await expect(wordmark).not.toHaveAttribute('data-cursor');
    }
  });

  test('shuffle resolves letters permanently in a random order', async ({
    page,
    pairingURL,
  }) => {
    await freezeStartup(page, 0.8);
    await page.goto(pairingURL);
    const wordmark = page.locator('.startup .wordmark');
    let previous: number[] = [];
    let elapsed = 0;
    const order: number[] = [];
    for (let count = 0; count <= 11; count++) {
      const at = count * 110 + 32;
      await page.clock.runFor(at - elapsed);
      elapsed = at;
      const text = (await wordmark.getAttribute('data-text'))!;
      const locked = [...text].flatMap((letter, index) =>
        letter === 'CODEXOMETER'[index] ? [index] : [],
      );
      expect(locked.length).toBe(count);
      for (const index of previous) expect(locked).toContain(index);
      order.push(...locked.filter((index) => !previous.includes(index)));
      previous = locked;
    }
    expect(order).not.toEqual([...Array(11).keys()]);
  });

  test('resizing preserves the entrance and fits the final mobile header', async ({
    page,
    pairingURL,
  }) => {
    await freezeStartup(page, 0.8);
    await page.goto(pairingURL);
    await page.clock.runFor(600);
    const intro = page.locator('.startup');
    const wordmark = intro.locator('.wordmark');
    const before = await wordmark.getAttribute('data-text');
    await page.setViewportSize({ width: 360, height: 640 });
    await page.clock.runFor(16);
    await expect(intro).toHaveAttribute('data-entrance', 'shuffle');
    await expect(wordmark).toHaveAttribute('data-text', before!);
    const box = (await wordmark.boundingBox())!;
    expect(box.width).toBeCloseTo(360, 0);
    expect(box.y + box.height / 2).toBeCloseTo(320, 0);
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBe(360);
    await page.clock.runFor(1700);
    await expect(intro).toHaveCount(0);
    await expect(
      page.getByRole('link', { name: 'CODEXOMETER', exact: true }),
    ).toBeInViewport();
    await expect(page.getByLabel('Theme', { exact: true })).toBeInViewport();
  });

  for (const input of ['key', 'click'] as const) {
    test(`${input} skips without forwarding an action to the dashboard`, async ({
      page,
      pairingURL,
    }) => {
      await freezeStartup(page, 0.4);
      await page.goto(pairingURL);
      await expect(page.locator('.startup')).toBeVisible();
      await expect(page.locator('.connection .lamp')).toHaveClass(/lit/);
      await page.evaluate(() => {
        location.hash = '#/sessions';
      });
      await expect(page.locator('.session-row')).toHaveCount(2);
      if (input === 'key') await page.keyboard.press('ArrowDown');
      else await page.mouse.click(40, 30); // The hidden header link lies underneath.
      await expect(page.locator('.startup')).toHaveCount(0);
      await expect(page.locator('main')).toBeVisible();
      await expect(page).toHaveURL(/#\/sessions$/);
      await expect(page.locator('.session-row').first()).toHaveClass(
        /selected/,
      );
      await page.clock.runFor(5000);
      await expect(page.locator('.startup')).toHaveCount(0);
    });
  }

  test('modified startup shortcuts pass through without skipping the intro', async ({
    page,
    pairingURL,
  }) => {
    await freezeStartup(page, 0.4);
    await page.goto(pairingURL);
    const intro = page.locator('.startup');
    await expect(intro).toBeVisible();
    await page.evaluate(() => {
      document.addEventListener('keydown', (event) => {
        document.documentElement.dataset.shortcutKey = event.key;
        document.documentElement.dataset.shortcutPrevented = String(
          event.defaultPrevented,
        );
      });
    });
    for (const shortcut of [
      'Shift+Tab',
      'Control+ArrowLeft',
      'Alt+ArrowRight',
      'Meta+k',
      'Shift',
    ]) {
      await page.keyboard.press(shortcut);
      await expect(intro).toBeVisible();
      await expect(page.locator('html')).toHaveAttribute(
        'data-shortcut-prevented',
        'false',
      );
    }
    // An unmodified key still skips and is consumed before reaching the app.
    await page.keyboard.press('ArrowDown');
    await expect(intro).toHaveCount(0);
    await expect(page.locator('html')).toHaveAttribute(
      'data-shortcut-key',
      'Shift',
    );
  });

  test('reduced motion opens directly and can also cancel an active intro', async ({
    page,
    pairingURL,
  }) => {
    await freezeStartup(page, 0.4);
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto(pairingURL);
    await expect(page.locator('.startup')).toHaveCount(0);
    await expect(
      page.getByRole('link', { name: 'CODEXOMETER', exact: true }),
    ).toBeVisible();
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await expect(page.locator('.startup')).toHaveCount(0);
    await page.reload();
    await expect(page.locator('.startup')).toBeVisible();
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await expect(page.locator('.startup')).toHaveCount(0);
  });

  test('the intro and header use the saved theme and survive stream reconnection', async ({
    page,
    pairingURL,
  }) => {
    await freezeStartup(page, 0.4);
    await page.addInitScript(() =>
      localStorage.setItem('codexometer.web.theme', 'rust'),
    );
    const snapshot = {
      version: 'startup-test',
      meters: [],
      credits: [],
      sessions: [],
    };
    await mockStream(page, snapshot);
    await page.goto(pairingURL);
    await expect(page.locator('.shell')).toHaveAttribute('data-theme', 'rust');
    const accent = await page
      .locator('.brand')
      .evaluate((element) => getComputedStyle(element).color);
    expect(
      await page
        .locator('.startup')
        .evaluate((element) => getComputedStyle(element).color),
    ).toBe(accent);
    await expect(page.locator('.connection .lamp')).toHaveClass(/lit/);
    await page.evaluate(
      (snapshot) =>
        window.dispatchEvent(
          new CustomEvent('test-snapshot', { detail: snapshot }),
        ),
      { ...snapshot, version: 'fresh-during-intro' },
    );
    await expect(page.locator('footer')).toContainText('fresh-during-intro');
    await page.keyboard.press('Escape');
    await expect(page.locator('footer')).toBeVisible();
    await page.evaluate(() =>
      window.dispatchEvent(new Event('test-disconnect')),
    );
    await expect(page.locator('.connection .lamp')).not.toHaveClass(/lit/);
    await page.clock.runFor(2600);
    await expect(page.locator('html')).toHaveAttribute(
      'data-test-streams',
      '2',
    );
    await expect(page.locator('.connection .lamp')).toHaveClass(/lit/);
    await expect(page.locator('.startup')).toHaveCount(0);
  });
});
