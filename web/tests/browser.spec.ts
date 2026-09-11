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
  expect(errors).toEqual([]);
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
