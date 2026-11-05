// Rasterise actual Go TUI cells, not the experimental web frontend.
// All data comes from test-only fixtures; no server, login or live requests.
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import {
  readFile,
  writeFile,
  mkdir,
  mkdtemp,
  readdir,
  rm,
} from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const output = join(root, 'assets/screenshots');
const fonts = 'tools/screenshots/fonts/';
const sources = [
  'internal/ui/documentation_screenshots_test.go',
  'web/scripts/screenshots.mjs',
  'web/package-lock.json',
  fonts + 'DejaVuSansMono.ttf',
  fonts + 'DejaVuSansMono-Bold.ttf',
];
const sha = (bytes) => createHash('sha256').update(bytes).digest('hex');
const version = (
  await readFile(join(root, 'internal/version/VERSION'), 'utf8')
).trim();
const sourceHashes = Object.fromEntries(
  await Promise.all(
    sources.map(async (path) => {
      const bytes = await readFile(join(root, path));
      // A Windows checkout may use CRLF; equivalent source still has one hash.
      return [
        path,
        sha(
          path.endsWith('.ttf')
            ? bytes
            : bytes.toString('utf8').replaceAll('\r\n', '\n'),
        ),
      ];
    }),
  ),
);

async function checkManifest(frames) {
  const manifest = JSON.parse(
    await readFile(join(output, 'manifest.json'), 'utf8'),
  );
  if (
    manifest.version !== version ||
    JSON.stringify(manifest.sources) !== JSON.stringify(sourceHashes)
  ) {
    throw new Error(
      'Screenshot version/tooling changed. Run make screenshots and commit the images.',
    );
  }
  if (frames && manifest.images.length !== frames.length) {
    throw new Error('Screenshot scene list changed. Run make screenshots.');
  }
  for (const entry of manifest.images) {
    if (!/^[a-z-]+\.png$/.test(entry.file))
      throw new Error('Invalid screenshot filename');
    if (sha(await readFile(join(output, entry.file))) !== entry.pngSHA256) {
      throw new Error(
        `${entry.file} changed without updating the manifest. Run make screenshots.`,
      );
    }
    if (
      frames &&
      !frames.some(
        ({ bytes, frame }) =>
          entry.file === `${frame.name}.png` &&
          entry.frameSHA256 === sha(bytes),
      )
    ) {
      throw new Error(
        `${entry.file} no longer matches the real TUI. Run make screenshots.`,
      );
    }
  }
  console.log(
    `Verified ${manifest.images.length} TUI screenshots for v${version}.`,
  );
}

if (process.argv.includes('--check-manifest')) {
  // Release-tag validation: no browser, npm installation or Go compilation.
  await checkManifest();
} else {
  const temporary = await mkdtemp(join(tmpdir(), 'codexometer-screenshots-'));
  let browser;
  try {
    execFileSync(
      'go',
      [
        'test',
        './internal/ui',
        '-run',
        '^TestDocumentationScreenshots$',
        '-count=1',
      ],
      {
        cwd: root,
        stdio: 'inherit',
        env: {
          ...process.env,
          CODEXOMETER_LANG: 'en-GB',
          TZ: 'UTC',
          CODEXOMETER_SCREENSHOT_DIR: temporary,
        },
      },
    );
    const frames = [];
    for (const name of (await readdir(temporary))
      .filter((name) => name.endsWith('.json'))
      .sort()) {
      const bytes = await readFile(join(temporary, name));
      frames.push({ bytes, frame: JSON.parse(bytes) });
    }
    if (frames.length === 0) throw new Error('No screenshot scenes exported');
    if (process.argv.includes('--check')) {
      await checkManifest(frames);
    } else {
      const { chromium } = await import('@playwright/test');
      browser = await chromium.launch();
      const page = await browser.newPage({ deviceScaleFactor: 2 });
      // Vendored, licensed fonts remove host-font/network dependencies.
      const regular = (
        await readFile(join(root, fonts + 'DejaVuSansMono.ttf'))
      ).toString('base64');
      const bold = (
        await readFile(join(root, fonts + 'DejaVuSansMono-Bold.ttf'))
      ).toString('base64');
      await page.route('**/*', (route) => route.abort());
      await page.setContent(`<style>
        @font-face { font-family: Terminal; src: url(data:font/ttf;base64,${regular}); }
        @font-face { font-family: Terminal; font-weight: bold; src: url(data:font/ttf;base64,${bold}); }
        html, body { margin: 0; background: #000; }
        canvas { display: block; }
      </style><canvas></canvas>`);
      await page.evaluate(async () => {
        await document.fonts.load('16px Terminal');
        await document.fonts.load('bold 16px Terminal');
        await document.fonts.ready;
      });
      await mkdir(output, { recursive: true });
      const images = [];
      for (const { bytes, frame } of frames) {
        const dimensions = await page.evaluate((frame) => {
          const canvas = document.querySelector('canvas');
          const ctx = canvas.getContext('2d');
          ctx.font = '16px Terminal';
          const cw = ctx.measureText('M').width;
          const ch = 21;
          const width = Math.ceil(frame.columns * cw);
          const height = frame.rows * ch;
          canvas.width = width * 2;
          canvas.height = height * 2;
          canvas.style.width = `${width}px`;
          canvas.style.height = `${height}px`;
          ctx.scale(2, 2);
          // Backgrounds first so wide glyphs/antialiasing aren't overwritten.
          for (const c of frame.cells) {
            ctx.fillStyle = c.bg;
            ctx.fillRect(c.x * cw, c.y * ch, c.width * cw + 0.01, ch);
          }
          for (const c of frame.cells) {
            if (!c.text.trim()) continue;
            ctx.font = `${c.italic ? 'italic ' : ''}${c.bold ? 'bold ' : ''}16px Terminal`;
            ctx.fillStyle = c.fg;
            ctx.globalAlpha = c.faint ? 0.5 : 1;
            ctx.fillText(c.text, c.x * cw, c.y * ch + 16);
            if (c.underline)
              ctx.fillRect(c.x * cw, c.y * ch + 18, c.width * cw, 1);
          }
          ctx.globalAlpha = 1;
          return { width, height };
        }, frame);
        await page.setViewportSize(dimensions);
        const file = `${frame.name}.png`;
        const png = await page
          .locator('canvas')
          .screenshot({ path: join(output, file) });
        images.push({
          file,
          caption: frame.caption,
          columns: frame.columns,
          rows: frame.rows,
          frameSHA256: sha(bytes),
          pngSHA256: sha(png),
        });
        console.log(
          `Captured ${file} (${frame.columns} × ${frame.rows} terminal cells).`,
        );
      }
      await writeFile(
        join(output, 'manifest.json'),
        JSON.stringify(
          {
            version,
            synthetic: true,
            sources: sourceHashes,
            images,
          },
          null,
          2,
        ) + '\n',
      );
    }
  } finally {
    await browser?.close();
    await rm(temporary, { recursive: true, force: true });
  }
}
