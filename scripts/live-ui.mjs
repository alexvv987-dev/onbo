import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { chromium } from 'playwright';
const base = process.env.ONBO_URL || 'http://localhost:8080';
const browser = await chromium.launch({ headless: true, executablePath: process.env.ONBO_BROWSER_PATH || undefined });
const context = await browser.newContext({ viewport: { width: 1440, height: 1050 }, recordVideo: { dir: 'output/video/raw', size: { width: 1440, height: 1050 } } });
const page = await context.newPage();
try {
  const config = await (await context.request.get(`${base}/api/config`)).json();
  assert.equal(config.llmMode, 'gigachat');
  await page.goto(base);
  await page.getByRole('button', { name: 'Открыть мой план' }).click();
  await page.getByText('GigaChat', { exact: true }).waitFor();
  async function ask(message) {
    await page.getByLabel('Сообщение Онбо').fill(message);
    const [response] = await Promise.all([
      page.waitForResponse(response => response.url().endsWith('/api/chat') && response.request().method() === 'POST', { timeout: 60000 }),
      page.getByRole('button', { name: 'Отправить сообщение' }).click(),
    ]);
    assert.equal(response.status(), 200);
    return response.json();
  }
  const answer = await ask('Как подготовиться к первой задаче? Найди инструкцию моей команды.');
  assert.ok(answer.message.length > 0);
  assert.ok(answer.sources.length > 0);
  assert.ok(answer.sources.every(source => !source.id.includes('analytics')));
  const draft = await ask('Подготовь черновик заявки на доступ к учебному репозиторию training-repo. Причина: подготовка к первой учебной задаче. Пока не отправляй.');
  assert.ok(draft.draft?.id);
  assert.equal(draft.draft.resourceId, 'training-repo');
  await page.getByRole('button', { name: 'Подтвердить и отправить', exact: true }).last().click();
  const card = page.locator('.request-card').filter({ has: page.getByRole('button', { name: 'Выдать доступ', exact: true }) }).first();
  await card.waitFor();
  await page.screenshot({ path: 'reports/screenshots/gigachat-live.png', fullPage: true });
  const requests = await (await context.request.get(`${base}/api/requests`)).json();
  const created = requests.find(request => request.draftId === draft.draft.id);
  assert.equal(created.status, 'pending');
  const [grantResponse] = await Promise.all([
    page.waitForResponse(response => response.url().endsWith(`/api/demo/requests/${created.id}/status`) && response.request().method() === 'POST'),
    page.locator('.request-card').filter({ hasText: created.id }).getByRole('button', { name: 'Выдать доступ' }).click(),
  ]);
  assert.equal(grantResponse.status(), 200);
  assert.equal((await grantResponse.json()).status, 'granted');
  await page.getByRole('button', { name: 'Задача выполнена' }).waitFor();
  await page.locator('.draft-card').last().screenshot({ path: 'reports/screenshots/draft-card.png' });
  await page.locator('.request-card').filter({ hasText: created.id }).screenshot({ path: 'reports/screenshots/result-card.png' });
  await fs.writeFile('reports/live-ui.json', JSON.stringify({ passed: true, mode: config.llmMode, sourceIds: answer.sources.map(source => source.id), draft: true, serviceResult: created.status, requestId: created.id, granted: true }, null, 2));
  console.log('PASS real GigaChat UI: source, draft, explicit confirmation, service result and granted plan.');
} finally {
  const video = page.video(); await context.close();
  if (video) await video.saveAs('output/video/onbo-gigachat.webm');
  await browser.close();
}
