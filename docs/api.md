# Контракт MVP

Все данные синтетические. JSON использует camelCase, ошибки — `{ "error": "понятное сообщение" }` с соответствующим HTTP-кодом. Все `/api/*`, кроме profiles и создания session, требуют cookie-сессию; изменяющие операции проверяют Origin. Идентичность задаётся сессией, не JSON-полями.

- `GET /api/profiles` → `User[]` (два демонстрационных профиля: `intern-platform`, `intern-analytics`).
- `GET /api/config` → `{demoMode,llmMode}`; интерфейс честно показывает режим модели, инструменты согласования видны только при demoMode.
- `GET /api/session` → `User`; `POST /api/session {profileId}` → `User`; `DELETE /api/session` → 204.
- `GET /api/plan` → `Plan`; `POST /api/tasks/{id}/complete {}` → `Plan` (только доступные ручные шаги).
- `GET /api/resources` → `Resource[]`; один ресурс `training-repo` только для команды платформы.
- `POST /api/drafts {resourceId,reason}` → `Draft` (неизменяемый черновик).
- `POST /api/drafts/{id}/confirm {confirmed:true}` → `AccessRequest`; без явного подтверждения ошибка. Повтор возвращает ту же операцию.
- `GET /api/requests` → `AccessRequest[]`; `POST /api/requests/{id}/refresh {}` → `AccessRequest`.
- `POST /api/demo/requests/{id}/status {status}` → `AccessRequest`, только при DEMO_MODE=true, status `granted` или `rejected`; обновляет имитацию и локальный результат.
- `GET /api/messages` → `Message[]`; `POST /api/chat {message}` → `ChatReply`; сохраняются вопрос и ответ, профиль из сессии. Ошибка LLM возвращает 502/504 и не выдаётся за ответ модели.
- `GET /api/mcp/tools` → каталог локальных MCP-действий текущего прототипа.
- `POST /api/events {name,metadata}` → 204; сохраняет обезличенное продуктовое событие текущей сессии.
- `GET /healthz` → 200 при готовности.

Типы находятся в `internal/core/types.go`. UI использует message из ChatReply как text assistant-сообщения. Статусы задач: `todo`, `blocked`, `pending`, `done`, `rejected`. Статусы заявок: `submitting` (исход ещё неизвестен), `pending`, `granted`, `rejected`. Только granted завершает шаг доступа. Два первых шага ручные, третий автоматический, четвёртый ручной после granted.

Имитация HTTP: `POST /requests {key,userId,resourceId}` → `{id,status,message}`; повтор того же key возвращает прежнюю заявку. `GET /requests/by-key/{key}` → тот же результат или404. `POST /requests/{id}/status {status}` → результат. Запросы защищены заголовком `X-Mock-Token`, токен передается только между сервисами. Состояние имитации сохраняется в PostgreSQL.
