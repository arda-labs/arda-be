#!/usr/bin/env node
// Business logic must obtain financial dates from arda-businessdate.
// The baseline contains only pre-existing technical timestamps/IDs; new uses
// fail this gate until they are removed or deliberately reviewed.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const roots = ['finance', 'loan', 'deposit', 'capital']
  .flatMap((service) => ['service', 'repository'].map((layer) =>
    path.join(root, 'apps', `${service}-service`, 'internal', layer)));
const baselinePath = path.join(root, 'scripts', 'no-wallclock-baseline.json');
const baseline = JSON.parse(fs.readFileSync(baselinePath, 'utf8'));
const findings = [];

function walk(dir) {
  if (!fs.existsSync(dir)) return;
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(fullPath);
    else if (entry.isFile() && entry.name.endsWith('.go') && !entry.name.endsWith('_test.go')) {
      const lines = fs.readFileSync(fullPath, 'utf8').split(/\r?\n/);
      lines.forEach((line, index) => {
        if (/\btime\.Now\s*\(/.test(line)) {
          findings.push(`${path.relative(root, fullPath).replaceAll('\\', '/')}:${index + 1}`);
        }
      });
    }
  }
}

roots.forEach(walk);
findings.sort();
const expected = [...baseline.entries].map((entry) => entry.location).sort();
const unexpected = findings.filter((location) => !expected.includes(location));
const stale = expected.filter((location) => !findings.includes(location));

if (unexpected.length || stale.length) {
  console.error('Business-logic wall-clock baseline mismatch. Remove new uses or review/update the baseline.');
  if (unexpected.length) console.error(`New time.Now() uses:\n${unexpected.map((x) => `  ${x}`).join('\n')}`);
  if (stale.length) console.error(`Stale baseline entries:\n${stale.map((x) => `  ${x}`).join('\n')}`);
  process.exit(1);
}

console.log(`No new time.Now() uses in finance/loan/deposit/capital service or repository code (${findings.length} reviewed baseline entries).`);
