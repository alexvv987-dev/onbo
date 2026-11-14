import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
const base = process.env.ONBO_URL || 'http://localhost:8080';
const login = await fetch(`${base}/api/session`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ profileId: 'intern-platform' }) });
assert.equal(login.status, 200);
const cookie = login.headers.get('set-cookie')?.split(';')[0];
assert.ok(cookie);
const latency = [];
for (let i = 0; i < 20; i++) {
  const start = performance.now();
  const response = await fetch(`${base}/api/plan`, { headers: { Cookie: cookie } });
  assert.equal(response.status, 200);
  await response.json();
  latency.push(Number((performance.now() - start).toFixed(2)));
}
const sorted = [...latency].sort((a, b) => a - b);
const report = { operation: 'GET /api/plan', samples: latency.length, latencyMs: latency, p95Ms: sorted[Math.ceil(sorted.length * 0.95) - 1], maxMs: sorted.at(-1), scope: 'Local API roundtrip with cookie session, Redis and PostgreSQL. Does not measure LLM latency or replace a user-interface responsiveness study.' };
await fs.writeFile('reports/performance.json', JSON.stringify(report, null, 2));
console.log(JSON.stringify({ samples: report.samples, p95Ms: report.p95Ms, maxMs: report.maxMs }));
assert.ok(report.maxMs < 1000, 'Simple local plan operation should complete in under one second');
