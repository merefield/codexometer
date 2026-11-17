import assert from 'node:assert/strict';
import test from 'node:test';
import { auditFindings } from './audit.mjs';

const url = 'https://github.com/advisories/GHSA-fc5q-39xc-vjpf';
const now = Date.parse('2026-11-17T12:00:00Z');
function fixture() {
  return {
    report: {
      auditReportVersion: 2,
      vulnerabilities: {
        vite: { via: ['tinyglobby'] },
        tinyglobby: { via: ['picomatch'] },
        picomatch: {
          nodes: ['node_modules/picomatch'],
          via: [{ name: 'picomatch', severity: 'high', url }],
        },
      },
    },
    lock: {
      packages: { 'node_modules/picomatch': { version: '4.0.7', dev: true } },
    },
  };
}

test('excepts only the approved advisory, including its transitive effects', () => {
  const { report, lock } = fixture();
  assert.deepEqual(auditFindings(report, lock, now), [
    { name: 'picomatch', url, severity: 'high', excepted: true },
  ]);
});

test('other advisories on the same package remain blocking', () => {
  const { report, lock } = fixture();
  report.vulnerabilities.picomatch.via.push({
    name: 'picomatch',
    severity: 'critical',
    url: 'https://example.test/another',
  });
  const findings = auditFindings(report, lock, now);
  assert.equal(findings.length, 2);
  assert.equal(findings[1].excepted, false);
  assert.equal(findings[1].severity, 'critical');
});

test('exception expires at its UTC deadline, not after it', () => {
  const { report, lock } = fixture();
  const end = Date.parse('2026-12-17T00:00:00Z');
  assert.equal(auditFindings(report, lock, end - 1)[0].excepted, true);
  assert.equal(auditFindings(report, lock, end)[0].excepted, false);
});

test('a severity escalation requires a fresh review', () => {
  const { report, lock } = fixture();
  report.vulnerabilities.picomatch.via[0].severity = 'critical';
  assert.equal(auditFindings(report, lock, now)[0].excepted, false);
});

test('runtime, missing, changed and mixed dependency instances are not excepted', () => {
  for (const instance of [
    { version: '4.0.7', dev: false },
    { version: '4.0.8', dev: true },
    undefined,
  ]) {
    const { report, lock } = fixture();
    lock.packages['node_modules/picomatch'] = instance;
    assert.equal(auditFindings(report, lock, now)[0].excepted, false);
  }
  const { report, lock } = fixture();
  report.vulnerabilities.picomatch.nodes.push(
    'node_modules/another/node_modules/picomatch',
  );
  lock.packages['node_modules/another/node_modules/picomatch'] = {
    version: '4.0.7',
  };
  assert.equal(auditFindings(report, lock, now)[0].excepted, false);
});

test('a different package cannot borrow the exception', () => {
  const { report, lock } = fixture();
  report.vulnerabilities.other = {
    nodes: ['node_modules/picomatch'],
    via: [{ name: 'other', severity: 'high', url }],
  };
  assert.equal(auditFindings(report, lock, now)[1].excepted, false);
});

test('clean reports pass; malformed, error and unresolved reports fail closed', () => {
  assert.deepEqual(
    auditFindings({ auditReportVersion: 2, vulnerabilities: {} }, {}, now),
    [],
  );
  for (const report of [
    {},
    { error: 'offline' },
    { auditReportVersion: 1, vulnerabilities: {} },
    { auditReportVersion: 2, vulnerabilities: [] },
    { auditReportVersion: 2, vulnerabilities: 'invalid' },
  ])
    assert.throws(() => auditFindings(report, {}, now));
  for (const via of [
    ['missing'],
    ['vite'],
    [],
    [{ severity: 'unexpected', url }],
  ]) {
    const { report, lock } = fixture();
    report.vulnerabilities.vite.via = via;
    assert.throws(() => auditFindings(report, lock, now));
  }
});
