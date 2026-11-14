import fs from 'node:fs';
import path from 'node:path';

// Go reports statement blocks, not a line percentage. Map instrumented ranges
// to unique source lines and merge hits before combining with V8 line counts.
const files = new Map();
for (const row of fs.readFileSync('reports/go.cover', 'utf8').trim().split(/\r?\n/).slice(1)) {
  const match = row.match(/^(.+):(\d+)\.(\d+),(\d+)\.(\d+)\s+\d+\s+(\d+)$/);
  if (!match) throw new Error(`Invalid Go coverage row: ${row}`);
  const [, file, start, , end, endColumn, hits] = match;
  if (file.endsWith('_test.go')) continue;
  const lines = files.get(file) ?? new Map();
  const last = Number(end) - (Number(endColumn) === 1 ? 1 : 0);
  for (let line = Number(start); line <= last; line++) lines.set(line, (lines.get(line) ?? false) || Number(hits) > 0);
  files.set(file, lines);
}
const go = { total: 0, covered: 0 };
const byFile = {};
for (const [file, lines] of files) {
  const result = { total: lines.size, covered: [...lines.values()].filter(Boolean).length };
  go.total += result.total; go.covered += result.covered;
  byFile[file] = result;
}
const frontend = JSON.parse(fs.readFileSync('web/coverage/coverage-summary.json', 'utf8')).total.lines;
const web = { total: frontend.total, covered: frontend.covered };
const total = { total: go.total + web.total, covered: go.covered + web.covered };
for (const result of [go, web, total]) result.percent = result.total ? Number((100 * result.covered / result.total).toFixed(2)) : 0;
const report = {
  generatedAt: new Date().toISOString(),
  methodology: 'Go: unique lines in instrumented coverprofile ranges (end column 1 is exclusive), union of hits for overlapping blocks. React: V8 executable-line coverage. Weighted total by line counts. Includes application commands and mock service; excludes dependencies, generated assets and tests.',
  go, frontend: web, total, goFiles: byFile,
};
fs.mkdirSync('reports', { recursive: true });
fs.writeFileSync('reports/coverage.json', JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify({ go, frontend: web, total }, null, 2));
if (total.total === 0 || total.covered / total.total <= 0.8) {
  console.error('FAIL: application line coverage must be strictly greater than 80%.');
  process.exitCode = 1;
}
