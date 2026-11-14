import fs from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { spawnSync } from 'node:child_process';

// Use the Codex bundled @oai/artifact-tool; no global installation is needed.
const moduleName = process.env.ARTIFACT_TOOL_MODULE;
const { Presentation } = await import(moduleName ? pathToFileURL(moduleName).href : '@oai/artifact-tool');
const deck = Presentation.create({ slideSize: { width: 1280, height: 720 } });
const color = { ink: '#25382f', muted: '#64746b', green: '#16664f', paper: '#f5f6f2' };
function text(slide, value, x, y, width, height, size = 28, bold = false, fill = color.ink) {
  const shape = slide.shapes.add({ geometry: 'textbox', position: { left: x, top: y, width, height }, fill: 'none', line: { fill: 'none', width: 0 } });
  shape.text = value;
  shape.text.style = { fontFamily: 'Arial', fontSize: size, bold, color: fill };
  return shape;
}
function slide(title) {
  const s = deck.slides.add(); s.background.fill = color.paper;
  text(s, title, 72, 60, 1136, 80, 46, true);
  text(s, `Онбо · Учебный прототип · ${deck.slides.items.length}`, 72, 670, 1136, 24, 16, false, color.muted);
  return s;
}
const cover = slide('Онбо');
text(cover, 'Помощник стажёра\nс первого дня', 72, 180, 1090, 190, 64, true, color.green);
text(cover, 'План адаптации, ответы по инструкциям\nи заявка на доступ в одном интерфейсе', 72, 420, 1090, 110, 32);
text(cover, 'Hack&Change 2026 · AI-buddy\ngithub.com/alexvv987-dev/onbo\nВидео: output/video/onbo-gigachat.webm', 72, 565, 1090, 90, 20, false, color.muted);
cover.speakerNotes.text = 'Репозиторий приватный: для проверки нужен доступ. Все корпоративные данные и сервисы синтетические. Видео и запуск доступны через README.';

const journey = slide('Путь стажёра');
text(journey, '01  План первых дней', 72, 180, 1100, 55, 34, true, color.green);
text(journey, 'Понятные шаги, сохранённый прогресс и следующее действие', 72, 240, 1100, 60, 28);
text(journey, '02  Ответ с источником', 72, 335, 1100, 55, 34, true, color.green);
text(journey, 'Инструкция своей команды и подтверждающий фрагмент', 72, 395, 1100, 60, 28);
text(journey, '03  Выполненное действие', 72, 490, 1100, 55, 34, true, color.green);
text(journey, 'Подтверждение заявки, номер сервиса и актуальный статус', 72, 550, 1100, 65, 28);

const action = slide('Результат действия');
text(action, 'Стажёр подтверждает\nресурс и причину.\n\nСервис возвращает\nномер заявки\nи статус доступа.', 72, 190, 320, 340, 30);
text(action, 'Тестовые данные\nи имитация сервиса', 72, 560, 310, 70, 20, false, color.muted);
action.images.add({ blob: new Uint8Array(await fs.readFile('reports/screenshots/draft-card.png')), contentType: 'image/png', alt: 'Подтверждённая заявка в работающем прототипе', fit: 'contain', position: { left: 430, top: 150, width: 775, height: 250 } });
action.images.add({ blob: new Uint8Array(await fs.readFile('reports/screenshots/result-card.png')), contentType: 'image/png', alt: 'Номер сервиса и выданный доступ', fit: 'contain', position: { left: 430, top: 410, width: 775, height: 210 } });

const architecture = slide('Архитектура и интеграции');
text(architecture, 'React + TypeScript', 72, 180, 540, 60, 35, true, color.green);
text(architecture, 'Чат, план, источники и подтверждение', 72, 250, 540, 110, 28);
text(architecture, 'Go API', 700, 180, 500, 60, 35, true, color.green);
text(architecture, 'Права, инструменты агента,\nпроверка аргументов и повторов', 700, 250, 500, 120, 28);
text(architecture, 'PostgreSQL и Redis', 72, 420, 540, 60, 35, true, color.green);
text(architecture, 'Состояние и инструкции в PostgreSQL.\nСессии и лимиты в Redis.', 72, 490, 540, 120, 27);
text(architecture, 'GigaChat и HTTP-сервис', 700, 420, 500, 60, 35, true, color.green);
text(architecture, 'Модель выбирает инструмент.\nСервис возвращает номер и статус.', 700, 490, 500, 120, 27);
architecture.speakerNotes.text = 'Пять инструментов: get_onboarding_plan, search_knowledge, list_access_resources, draft_access_request, get_access_request_status. Подтверждение отправки — отдельная серверная операция. OAuth, TLS и нативные функции: https://developers.sber.ru/docs/ru/gigachat/api/reference/rest/gigachat-api';

const coverage = JSON.parse(await fs.readFile('reports/coverage.json', 'utf8'));
let live = null;
try { live = JSON.parse((await fs.readFile('reports/gigachat-container.json', 'utf8')).replace(/^\uFEFF/, '')); } catch {}
const verification = slide('Проверка работающего прототипа');
text(verification, `${coverage.total.percent}%`, 72, 175, 600, 120, 86, true, color.green);
text(verification, 'Покрытие инструментированных строк\nGo и исполняемых строк React', 72, 305, 610, 95, 28);
text(verification, 'Права двух профилей, подтверждение,\nповтор и неоднозначный таймаут.\nСохранение после перезапуска.', 72, 455, 1080, 140, 28);
text(verification, live ? `${live.passed}/${live.total}` : 'Live eval', 800, 190, 350, 100, 68, true, color.green);
text(verification, 'Проверки настоящего\nGigaChat из контейнера', 800, 305, 350, 95, 26);
verification.speakerNotes.text = 'Снимок воспроизводимых отчётов в docs/verification.md. Go: уникальные строки coverprofile-блоков. React: V8. Результат ограничен тестовой матрицей и не доказывает качество всех ответов. Отдельный браузерный прогон использует явно обозначенную подмену LLM.';

const effect = slide('Проверка пользы на стажёрах');
text(effect, 'Гипотеза', 72, 180, 1100, 55, 34, true, color.green);
text(effect, 'Стажёр быстрее выполняет организационную задачу\nи реже обращается за повторным объяснением.', 72, 250, 1100, 110, 30);
text(effect, 'Пилот', 72, 410, 1100, 55, 34, true, color.green);
text(effect, 'Равноценные задачи с помощником и по исходным материалам.\nИзмеряем время, успешность и обращения за помощью.\nПользовательское исследование ещё не проведено.', 72, 480, 1100, 150, 28);
effect.speakerNotes.text = 'Протокол: docs/product-validation.md. Менять порядок условий между участниками, отделять ожидание сервиса от активного времени, указывать размер и ограничения выборки.';

await fs.mkdir('output/pdf', { recursive: true });
await fs.mkdir('tmp/slides', { recursive: true });
for (const [index, s] of deck.slides.items.entries()) {
  const png = await deck.export({ slide: s, format: 'png', scale: 1 });
  await fs.writeFile(`tmp/slides/slide-${index + 1}.png`, new Uint8Array(await png.arrayBuffer()));
}
try {
  const pdf = await deck.export({ format: 'pdf' });
  await fs.writeFile('output/pdf/onbo-mvp.pdf', new Uint8Array(await pdf.arrayBuffer()));
} catch {
  console.warn('Native PDF font export unavailable; assembling the rendered slides with ReportLab.');
  const fallback = spawnSync(process.env.ONBO_PYTHON || 'python', ['scripts/slides-to-pdf.py'], { stdio: 'inherit' });
  if (fallback.status !== 0) throw new Error('PDF assembly failed');
}
console.log(`Created ${deck.slides.items.length}-page presentation at output/pdf/onbo-mvp.pdf`);
