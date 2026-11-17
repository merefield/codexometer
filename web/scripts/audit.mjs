// No packages are installed or executed: audit the committed lockfile only.
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const advisory = 'https://github.com/advisories/GHSA-fc5q-39xc-vjpf';
const expires = Date.parse('2026-12-17T00:00:00Z');
const severities = ['info', 'low', 'moderate', 'high', 'critical'];

// Follow npm's meta-vulnerabilities to their actual advisories. Never ignore
// whole packages: a second advisory in the same dependency must still block.
export function auditFindings(report, lock, now = Date.now()) {
  if (
    report.error ||
    report.auditReportVersion !== 2 ||
    !report.vulnerabilities ||
    typeof report.vulnerabilities !== 'object' ||
    Array.isArray(report.vulnerabilities)
  )
    throw new Error('npm audit did not return a valid vulnerability report');
  const findings = new Map();
  function visit(name, ancestors = []) {
    const entry = report.vulnerabilities[name];
    if (
      !entry ||
      ancestors.includes(name) ||
      !Array.isArray(entry.via) ||
      !entry.via.length
    )
      throw new Error(`Unresolved audit dependency: ${name}`);
    for (const via of entry.via) {
      if (typeof via === 'string') {
        visit(via, [...ancestors, name]);
        continue;
      }
      if (!via || !severities.includes(via.severity) || !via.url)
        throw new Error(`Unrecognised audit advisory: ${name}`);
      const nodes = entry.nodes;
      const excepted =
        name === 'picomatch' &&
        via.name === name &&
        via.url === advisory &&
        via.severity === 'high' &&
        Number.isFinite(now) &&
        now < expires &&
        Array.isArray(nodes) &&
        nodes.length > 0 &&
        nodes.every(
          (path) =>
            lock.packages?.[path]?.version === '4.0.7' &&
            lock.packages[path].dev === true,
        );
      findings.set(`${name}:${via.url}`, {
        name,
        url: via.url,
        severity: via.severity,
        excepted,
      });
    }
  }
  for (const name of Object.keys(report.vulnerabilities)) visit(name);
  return [...findings.values()];
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    const result = spawnSync('npm', ['audit', '--json', '--audit-level=high'], {
      cwd: fileURLToPath(new URL('..', import.meta.url)),
      encoding: 'utf8',
      timeout: 120_000,
      maxBuffer: 10 * 1024 * 1024,
    });
    if (result.error || ![0, 1].includes(result.status))
      throw new Error(
        `npm audit failed: ${result.error?.message || result.stderr || result.status}`,
      );
    const report = JSON.parse(result.stdout);
    const lock = JSON.parse(
      readFileSync(new URL('../package-lock.json', import.meta.url), 'utf8'),
    );
    const findings = auditFindings(report, lock);
    // Keep every original advisory and transitive dependency visible in CI.
    console.log(JSON.stringify(report, null, 2));
    let blocked = false;
    for (const finding of findings) {
      if (finding.excepted) {
        console.log(
          `TEMPORARY EXCEPTION until 2026-12-17 UTC: ${finding.url} (picomatch 4.0.7, dev-only; upstream issue #234)`,
        );
      } else if (['high', 'critical'].includes(finding.severity)) {
        console.error(`BLOCKED: ${finding.name}: ${finding.url}`);
        blocked = true;
      }
    }
    process.exitCode = blocked ? 1 : 0;
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
