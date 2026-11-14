import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { chromium } from 'playwright';

const base = process.env.ONBO_URL || 'http://localhost:8080';
const checks = [];
const record = (name) => { checks.push(name); console.log(`PASS ${name}`); };
await fs.mkdir('output/video', { recursive: true });
await fs.mkdir('reports/screenshots', { recursive: true });
const browser = await chromium.launch({ headless: true, executablePath: process.env.ONBO_BROWSER_PATH || undefined });
const context = await browser.newContext({ viewport: { width: 1440, height: 1050 }, recordVideo: { dir: 'output/video/raw', size: { width: 1440, height: 1050 } } });
const page = await context.newPage();
const errors = [];
page.on('pageerror', error => errors.push(error.message));
try {
  assert.equal((await context.request.get(`${base}/healthz`)).status(), 200);
  const config = await (await context.request.get(`${base}/api/config`)).json();
  assert.equal(config.llmMode, 'demo', 'Reproducible E2E uses explicit demo mode; real GigaChat is tested by cmd/eval.');
  await page.goto(base);
  await page.getByRole('button', { name: 'Открыть мой план' }).click();
  await page.getByRole('heading', { name: 'Первые шаги', exact: true }).waitFor();
  await page.screenshot({ path: 'reports/screenshots/workspace.png', fullPage: true });
  for (const id of ['team', 'instructions']) {
    const plan = await (await context.request.get(`${base}/api/plan`)).json();
    const task = plan.tasks.find(item => item.id === id);
    if (task.status !== 'done') {
      await page.locator('.task-item').filter({ hasText: task.title }).getByRole('button', { name: 'Ознакомился' }).click();
      await page.waitForFunction(() => !document.querySelector('button:disabled.text-button'));
    }
  }
  record('manual progress saved on server');
  await page.getByLabel('Сообщение Онбо').fill('Как подготовиться к первой задаче? Найди инструкцию.');
  await page.getByRole('button', { name: 'Отправить сообщение' }).click();
  await page.locator('.source-card').first().waitFor();
  assert.match(await page.locator('.source-label').first().textContent(), /Тестовая инструкция/);
  record('grounded answer and synthetic source');
  const before = await (await context.request.get(`${base}/api/requests`)).json();
  await page.getByLabel('Сообщение Онбо').fill('Подготовь заявку на доступ к учебному репозиторию для первой задачи');
  await page.getByRole('button', { name: 'Отправить сообщение' }).click();
  await page.getByRole('button', { name: 'Подтвердить и отправить', exact: true }).last().waitFor();
  let history = await (await context.request.get(`${base}/api/messages`)).json();
  const draft = history.findLast(item => item.draft)?.draft;
  assert.ok(draft?.id);
  const invalid = await context.request.post(`${base}/api/drafts/${draft.id}/confirm`, { data: { confirmed: false } });
  assert.equal(invalid.status(), 400);
  assert.equal((await (await context.request.get(`${base}/api/requests`)).json()).length, before.length);
  record('no submission without explicit confirmation');
  await page.screenshot({ path: 'reports/screenshots/draft.png', fullPage: true });
  await page.getByRole('button', { name: 'Подтвердить и отправить', exact: true }).last().click();
  await page.getByRole('button', { name: 'Выдать доступ', exact: true }).first().waitFor();
  const requests = await (await context.request.get(`${base}/api/requests`)).json();
  const created = requests.find(item => item.draftId === draft.id);
  assert.equal(created.status, 'pending');
  assert.match(created.id, /^REQ-/);
  const repeat = await (await context.request.post(`${base}/api/drafts/${draft.id}/confirm`, { data: { confirmed: true } })).json();
  assert.equal(repeat.id, created.id);
  assert.equal((await (await context.request.get(`${base}/api/requests`)).json()).length, before.length + 1);
  const pendingPlan = await (await context.request.get(`${base}/api/plan`)).json();
  assert.equal(pendingPlan.tasks.find(item => item.id === 'access').status, before.some(item => item.status === 'granted') ? 'done' : 'pending');
  record('service result displayed; pending does not complete access; repeat creates no duplicate');
  const [grantResponse] = await Promise.all([
    page.waitForResponse(response => response.url().endsWith(`/api/demo/requests/${created.id}/status`) && response.request().method() === 'POST'),
    page.locator('.request-card').filter({ hasText: created.id }).getByRole('button', { name: 'Выдать доступ' }).click(),
  ]);
  assert.equal(grantResponse.status(), 200);
  assert.equal((await grantResponse.json()).status, 'granted');
  await page.getByRole('button', { name: 'Задача выполнена' }).waitFor();
  const grantedPlan = await (await context.request.get(`${base}/api/plan`)).json();
  assert.equal(grantedPlan.tasks.find(item => item.id === 'access').status, 'done');
  assert.notEqual(grantedPlan.tasks.find(item => item.id === 'first-task').status, 'blocked');
  record('granted unlocks next step');
  await page.screenshot({ path: 'reports/screenshots/granted.png', fullPage: true });
  if (process.env.E2E_RESTART !== '0') {
    execFileSync('docker', ['compose', 'restart', 'app'], { stdio: 'pipe' });
    for (let attempt = 0; attempt < 30; attempt++) {
      try { if ((await context.request.get(`${base}/healthz`)).status() === 200) break; } catch {}
      await new Promise(resolve => setTimeout(resolve, 500));
    }
  }
  await page.reload();
  await page.locator('.request-card').filter({ hasText: created.id }).waitFor();
  assert.equal((await (await context.request.get(`${base}/api/plan`)).json()).tasks.find(item => item.id === 'access').status, 'done');
  record('progress, dialog and service result survive app restart');
  const crossSite = await context.request.post(`${base}/api/tasks/team/complete`, { data: {}, headers: { Origin: 'https://untrusted.example' } });
  assert.equal(crossSite.status(), 403);
  const other = await browser.newContext();
  await other.request.post(`${base}/api/session`, { data: { profileId: 'intern-analytics' } });
  assert.deepEqual(await (await other.request.get(`${base}/api/requests`)).json(), []);
  assert.deepEqual(await (await other.request.get(`${base}/api/resources`)).json(), []);
  assert.equal((await other.request.post(`${base}/api/requests/${created.id}/refresh`, { data: {} })).status(), 404);
  assert.equal((await other.request.post(`${base}/api/drafts/${draft.id}/confirm`, { data: { confirmed: true } })).status(), 404);
  assert.equal((await other.request.post(`${base}/api/drafts`, { data: { resourceId: 'training-repo', reason: 'test' } })).status(), 403);
  await other.close();
  record('second profile cannot read or operate foreign data; cross-site mutation rejected');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: 'reports/screenshots/mobile.png', fullPage: true });
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'No horizontal overflow on mobile');
  record('mobile layout fits viewport');
  assert.deepEqual(errors, []);
  await fs.writeFile('reports/e2e.json', JSON.stringify({ passed: true, checks, browserErrors: errors, mode: 'demo', appRestarted: process.env.E2E_RESTART !== '0' }, null, 2));
} finally {
  const video = page.video();
  await context.close();
  if (video) await video.saveAs('output/video/onbo-demo.webm');
  await browser.close();
}
