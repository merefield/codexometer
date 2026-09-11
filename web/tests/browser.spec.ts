import { test as base, expect } from '@playwright/test';
import { spawn } from 'node:child_process';
import { resolve } from 'node:path';

const test = base.extend<{ pairingURL: string }>({
  pairingURL: async ({}, use) => {
    const child = spawn(
      process.env.CODEXOMETER_TEST_BINARY ||
        resolve(
          '..',
          process.platform === 'win32' ? 'codexometer.exe' : 'codexometer',
        ),
      ['--web', '--demo'],
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
  await expect(page.locator('svg')).toHaveCount(2);
  await page
    .getByRole('link', { name: 'CONSUMPTION PACE', exact: true })
    .click();
  await expect(page.locator('.pace')).toHaveCount(2);
  await expect(page.getByText('−100 // OVER BUDGET')).toHaveCount(2);
  await expect(page.getByText('+100 // HEADROOM')).toHaveCount(2);
  await page
    .getByRole('link', { name: 'CONSUMPTION ZONE', exact: true })
    .click();
  await expect(page.locator('.consumption-zone')).toHaveCount(2);
  await expect(page.locator('.position-dot')).toHaveCount(2);
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
      summary: {},
      dailyUsageBuckets: [
        { startDate: '2026-09-10', tokens: 100 },
        { startDate: '2026-09-10', tokens: 250 },
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
    page.locator('.heat-cell[title="2026-09-10: 350 tokens"]'),
  ).toHaveCount(1);
  await expect(
    page.locator('.heat-cell[title="2026-09-09: 0 tokens"]'),
  ).toHaveCount(1);
  await page.getByText('Accessible data table').click();
  await expect(
    page.getByRole('row').filter({
      has: page.getByRole('cell', { name: '2026-09-10', exact: true }),
    }),
  ).toContainText('350');
  await page.getByLabel('Usage view').selectOption('monthly');
  await expect(
    page.getByRole('row').filter({
      has: page.getByRole('cell', { name: '2026-09', exact: true }),
    }),
  ).toContainText('370');
  await page.getByLabel('Usage view').selectOption('cumulative');
  await expect(
    page.getByRole('row').filter({
      has: page.getByRole('cell', { name: '2026-09-11', exact: true }),
    }),
  ).toContainText('370');
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

test('consumption zone plots bounded coordinates and handles missing windows', async ({
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
  await page
    .getByRole('link', { name: 'CONSUMPTION ZONE', exact: true })
    .click();
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

test('quota graphics use viewport height and keep compact navigation accessible', async ({
  page,
  pairingURL,
}) => {
  await page.setViewportSize({ width: 1280, height: 700 });
  await page.goto(pairingURL);
  for (const [name, graphic] of [
    ['BARS', '.gauge:not(.timeline)'],
    ['CONSUMPTION PACE', '.pace'],
    ['CONSUMPTION ZONE', '.zone-canvas'],
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
  await page
    .getByRole('link', { name: 'CONSUMPTION ZONE', exact: true })
    .click();
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
  await page.getByRole('button', { name: 'SHOW DETAIL' }).first().click();
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
  await page.getByLabel('Usage view').selectOption('monthly');
  await expect(page.locator('.chart')).toBeVisible();
  await page.getByLabel('Usage view').selectOption('cumulative');
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
        id: 'untrusted',
        directory: '/test',
        tokens: 100,
        agents: 0,
        status: 'INPUT NEEDED',
        contextKind: 'LAST REPLY',
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
  await page.getByRole('link', { name: 'FULL DETAIL →' }).click();
  await expect(page.locator('.full-detail pre').first()).toContainText('<img');
  await expect(
    page.locator('.full-detail img, .full-detail script, .full-detail iframe'),
  ).toHaveCount(0);
  await expect(
    page.getByRole('button', { name: /APPROVE|CONFIRM|SEND/ }),
  ).toHaveCount(0);
});
