import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { api, ApiError, errorText } from "./api";
import avitoMark from "./avito-mark.png";
import { trackMetric } from "./metrics";
import type {
  AccessRequest,
  Config,
  Draft,
  Message,
  Plan,
  User,
} from "./types";

const statusLabels: Record<string, string> = {
  todo: "Впереди",
  blocked: "Пока недоступно",
  pending: "Ожидает согласования",
  done: "Готово",
  granted: "Доступ выдан",
  rejected: "Нужна помощь",
  submitting: "Результат уточняется",
};
const suggestions = [
  "С чего начать первую задачу?",
  "Как получить доступ к репозиторию?",
  "К кому обратиться за помощью?",
];

function Arrow() {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M5 12h14m-6-6 6 6-6 6"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function TopicIcon({ index }: { index: number }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {index === 0 ? (
        <path d="M6 3h9l4 4v14H6V3Zm8 0v5h5M9 12h7M9 16h5" />
      ) : index === 1 ? (
        <path d="M15 14a5 5 0 1 0-5-5c0 .7.1 1.3.4 1.9L3 18.3V21h3v-3h3v-3l2.1-2.1c.8.7 1.8 1.1 3 1.1ZM16 7h.01" />
      ) : (
        <>
          <circle cx="12" cy="8" r="3.5" />
          <path d="M5 21v-2a7 7 0 0 1 14 0v2M19 5a3.5 3.5 0 0 1 0 7M22 21v-2a7 7 0 0 0-2-5" />
        </>
      )}
    </svg>
  );
}

export default function App() {
  const [profiles, setProfiles] = useState<User[]>([]);
  const [profileId, setProfileId] = useState("intern-platform");
  const [user, setUser] = useState<User | null>(null);
  const [config, setConfig] = useState<Config>({
    demoMode: false,
    llmMode: "gigachat",
  });
  const [plan, setPlan] = useState<Plan | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [requests, setRequests] = useState<AccessRequest[]>([]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [mobileView, setMobileView] = useState<"chat" | "plan">("chat");
  const chatScroll = useRef<HTMLDivElement>(null);
  const textarea = useRef<HTMLTextAreaElement>(null);

  async function loadWorkspace() {
    const [nextPlan, nextMessages, nextRequests] = await Promise.all([
      api.plan(),
      api.messages(),
      api.requests(),
    ]);
    setPlan(nextPlan);
    setMessages(nextMessages || []);
    setRequests(nextRequests || []);
  }

  async function boot() {
    setLoading(true);
    setError("");
    try {
      const [nextProfiles, nextConfig] = await Promise.all([
        api.profiles(),
        api.config(),
      ]);
      setProfiles(nextProfiles);
      setConfig(nextConfig);
      const current = await api.session().catch((failure: unknown) => {
        if (failure instanceof ApiError && failure.status === 401) return null;
        throw failure;
      });
      setUser(current);
      if (current) {
        setProfileId(current.id);
        await loadWorkspace();
        trackMetric("plan_opened");
      }
    } catch (failure) {
      setError(errorText(failure));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void boot();
  }, []);
  useEffect(() => {
    if (chatScroll.current) {
      chatScroll.current.scrollTop = chatScroll.current.scrollHeight;
    }
  }, [messages, busy]);

  async function signIn(id: string) {
    setBusy("profile");
    setError("");
    setNotice("");
    try {
      const current = await api.login(id);
      // Clear the previous identity before loading another user's workspace.
      setUser(current);
      setProfileId(current.id);
      setPlan(null);
      setMessages([]);
      setRequests([]);
      setInput("");
      await loadWorkspace();
    } catch (failure) {
      setError(errorText(failure));
    } finally {
      setBusy("");
    }
  }

  async function send(event: FormEvent) {
    event.preventDefault();
    const text = input.trim();
    if (!text || busy) return;
    setBusy("chat");
    setError("");
    setNotice("");
    try {
      const reply = await api.chat(text);
      trackMetric("question_asked");
      if (reply.sources?.length)
        trackMetric("source_shown", { count: String(reply.sources.length) });
      if (reply.draft)
        trackMetric("draft_created", { resourceId: reply.draft.resourceId });
      setMessages((previous) => [
        ...previous,
        { role: "user", text },
        {
          role: "assistant",
          text: reply.message,
          sources: reply.sources,
          draft: reply.draft,
        },
      ]);
      setInput("");
    } catch (failure) {
      setError(errorText(failure));
    } finally {
      setBusy("");
      textarea.current?.focus();
    }
  }

  async function complete(id: string) {
    setBusy(`task:${id}`);
    setError("");
    try {
      setPlan(await api.complete(id));
      trackMetric("task_completed", { taskId: id });
    } catch (failure) {
      setError(errorText(failure));
    } finally {
      setBusy("");
    }
  }

  async function confirm(draft: Draft) {
    setBusy(`draft:${draft.id}`);
    setError("");
    setNotice("");
    try {
      const result = await api.confirm(draft.id);
      trackMetric("request_confirmed", { requestId: result.id });
      setRequests((previous) => [
        ...previous.filter((item) => item.draftId !== result.draftId),
        result,
      ]);
      setPlan(await api.plan());
      setNotice(
        result.status === "submitting"
          ? "Ответ сервиса пока неизвестен. Проверьте результат — повтор не создаст новую заявку."
          : `Заявка ${result.id}: ${statusLabels[result.status] || result.status}.`,
      );
    } catch (failure) {
      setError(errorText(failure));
    } finally {
      setBusy("");
    }
  }

  async function updateRequest(
    request: AccessRequest,
    status?: "granted" | "rejected",
  ) {
    setBusy(`request:${request.id}`);
    setError("");
    setNotice("");
    try {
      const result = status
        ? await api.simulate(request.id, status)
        : await api.refresh(request.id);
      trackMetric(status ? "request_result" : "request_refreshed", {
        status: result.status,
      });
      setRequests((previous) =>
        previous.map((item) =>
          item.draftId === result.draftId ? result : item,
        ),
      );
      setPlan(await api.plan());
      setNotice(
        `Заявка ${result.id}: ${statusLabels[result.status] || result.status}.`,
      );
    } catch (failure) {
      setError(errorText(failure));
    } finally {
      setBusy("");
    }
  }

  const done = plan?.tasks.filter((task) => task.status === "done").length ?? 0;
  const taskCount = plan?.tasks.length ?? 4;

  return (
    <div className="app-shell">
      <div className="utility-bar">
        <div className="page-width utility-content">
          <span>Стажировка в Авито</span>
          <span className="demo-tag">Учебный прототип</span>
        </div>
      </div>
      <header className="topbar">
        <div className="page-width header-content">
          <a href="/" className="brand" aria-label="Онбо, главная">
            <img
              className="brand-mark"
              src={avitoMark}
              alt=""
              width={40}
              height={40}
            />
            <span className="brand-name">онбо</span>
          </a>
          <span className="brand-caption">Помощник стажёра</span>
          {user && (
            <nav className="header-nav" aria-label="Основная навигация">
              <a href="#workspace" onClick={() => setMobileView("chat")}>
                Помощник
              </a>
              <a href="#onboarding-plan" onClick={() => setMobileView("plan")}>
                Мой план
              </a>
            </nav>
          )}
          <div className="header-right">
            {user && (
              <label className="profile-control">
                <span className="avatar">{user.name.charAt(0)}</span>
                <span className="sr-only">Тестовый профиль</span>
                <select
                  aria-label="Тестовый профиль"
                  value={profileId}
                  disabled={!!busy || loading}
                  onChange={(event) => void signIn(event.target.value)}
                >
                  {profiles.map((profile) => (
                    <option key={profile.id} value={profile.id}>
                      {profile.name} · {profile.team}
                    </option>
                  ))}
                </select>
              </label>
            )}
          </div>
        </div>
      </header>

      {loading ? (
        <main className="welcome-card">
          <span className="spinner" />
          <p>Подготавливаем твоё рабочее пространство…</p>
        </main>
      ) : !user ? (
        <main className="login-layout">
          <div className="login-copy">
            <p className="eyebrow">Онбо для стажёров</p>
            <h1>
              Первый день
              <br />в новой команде
            </h1>
            <p>
              Найдите рабочие инструкции, получите доступы и узнайте, с какой
              задачи начать.
            </p>
            <div className="login-path">
              <div>
                <TopicIcon index={0} />
                <span>
                  <strong>Инструкции под рукой</strong>Ответы со ссылками на
                  материалы команды
                </span>
              </div>
              <div>
                <TopicIcon index={1} />
                <span>
                  <strong>Помощь с доступами</strong>От заявки до
                  подтверждённого результата
                </span>
              </div>
              <div>
                <TopicIcon index={2} />
                <span>
                  <strong>Понятный план</strong>Что уже готово и что делать
                  дальше
                </span>
              </div>
            </div>
          </div>
          <section className="login-card">
            <h2>Давай знакомиться</h2>
            <p>Выбери тестовый профиль, чтобы пройти путь стажёра.</p>
            <label htmlFor="initial-profile">Тестовый профиль</label>
            <select
              id="initial-profile"
              value={profileId}
              onChange={(event) => setProfileId(event.target.value)}
              disabled={!!busy}
            >
              {profiles.map((profile) => (
                <option key={profile.id} value={profile.id}>
                  {profile.name} · {profile.team}
                </option>
              ))}
            </select>
            <button
              className="primary-button button-primary"
              disabled={!!busy || !profiles.length}
              onClick={() => void signIn(profileId)}
            >
              {busy ? "Открываем…" : "Открыть мой план"}
              <Arrow />
            </button>
            <p className="test-note">
              Все имена, инструкции и сервисы в прототипе — тестовые. Это не
              регламенты Авито.
            </p>
          </section>
        </main>
      ) : (
        <main
          className="workspace page-width"
          id="workspace"
          data-mobile-view={mobileView}
        >
          <div className="workspace-intro">
            <p className="eyebrow">Личное пространство · {user.team}</p>
            <h1>На связи, {user.name.split(" ")[0]}.</h1>
            <p>Здесь всё, что поможет разобраться в первых задачах.</p>
          </div>
          <div
            className="workspace-tabs"
            aria-label="Разделы рабочего пространства"
          >
            <button
              type="button"
              aria-pressed={mobileView === "chat"}
              aria-controls="assistant-panel"
              onClick={() => setMobileView("chat")}
            >
              Помощник
            </button>
            <button
              type="button"
              aria-pressed={mobileView === "plan"}
              aria-controls="onboarding-plan"
              onClick={() => setMobileView("plan")}
            >
              Мой план{" "}
              <span>
                {done}/{taskCount}
              </span>
            </button>
          </div>
          <section
            className="chat-panel"
            id="assistant-panel"
            aria-label="Чат с Онбо"
          >
            <div className="section-heading">
              <div>
                <h2>Чат с Онбо</h2>
                <p>Ответы по материалам вашей команды</p>
              </div>
              <span className="model-tag">
                <span />
                {config.llmMode === "demo" ? "Демо-ответы" : "GigaChat"}
              </span>
            </div>
            <div
              className="chat-scroll"
              ref={chatScroll}
              aria-live="polite"
              aria-busy={busy === "chat"}
            >
              {!messages.length && (
                <div className="empty-chat">
                  <p className="empty-caption">Начнём с простого</p>
                  <h3>Что хотите узнать?</h3>
                  <p>Спросите о процессах команды или выберите вопрос ниже.</p>
                  <div className="suggestions">
                    {suggestions.map((text, index) => (
                      <button
                        key={text}
                        disabled={!!busy}
                        onClick={() => {
                          setInput(text);
                          textarea.current?.focus();
                        }}
                      >
                        <TopicIcon index={index} />
                        <span>{text}</span>
                      </button>
                    ))}
                  </div>
                </div>
              )}
              {messages.map((message, index) => (
                <article
                  className={`chat-message ${message.role === "user" ? "user-message" : "assistant-message"}`}
                  key={`${index}-${message.role}`}
                >
                  <span className="message-author">
                    {message.role === "user" ? "Ты" : "Онбо"}
                  </span>
                  <p className="message-text">{message.text}</p>
                  {!!message.sources?.length && (
                    <div className="sources" aria-label="Источники ответа">
                      {message.sources.map((source) => (
                        <div className="source-card" key={source.id}>
                          <div className="source-heading">
                            <span className="document-icon" aria-hidden="true">
                              ▤
                            </span>
                            <strong>{source.title}</strong>
                          </div>
                          <p>{source.excerpt}</p>
                          <span className="source-label">
                            {source.label || "Тестовая инструкция"}
                          </span>
                        </div>
                      ))}
                    </div>
                  )}
                  {message.draft &&
                    (() => {
                      const draft = message.draft;
                      const existing = requests.find(
                        (item) => item.draftId === draft.id,
                      );
                      const submitted =
                        existing && existing.status !== "submitting";
                      return (
                        <section
                          className="draft-card"
                          aria-label="Черновик заявки"
                        >
                          <div className="draft-heading">
                            <span className="mini-label">ЗАЯВКА НА ДОСТУП</span>
                            <span className="draft-status">
                              {submitted ? "Отправлена" : "Черновик"}
                            </span>
                          </div>
                          <h3>{draft.resourceName}</h3>
                          <p>{draft.reason}</p>
                          <p className="draft-explanation">
                            Проверь данные. Заявка будет отправлена только после
                            твоего подтверждения.
                          </p>
                          <button
                            className="primary-button button-primary"
                            disabled={!!busy || !!submitted}
                            onClick={() => void confirm(draft)}
                          >
                            {submitted
                              ? "Заявка отправлена"
                              : existing
                                ? "Восстановить отправку"
                                : busy === `draft:${draft.id}`
                                  ? "Отправляем…"
                                  : "Подтвердить и отправить"}
                            {!submitted && <Arrow />}
                          </button>
                        </section>
                      );
                    })()}
                </article>
              ))}
              {busy === "chat" && (
                <div className="thinking" role="status">
                  <span className="thinking-dots">
                    <i />
                    <i />
                    <i />
                  </span>
                  Онбо разбирается в вопросе…
                </div>
              )}
            </div>
            <form className="composer" onSubmit={(event) => void send(event)}>
              <label htmlFor="message-input" className="sr-only">
                Сообщение Онбо
              </label>
              <textarea
                ref={textarea}
                id="message-input"
                value={input}
                onChange={(event) => setInput(event.target.value)}
                placeholder="Задайте вопрос об инструкциях, доступах или первых задачах"
                rows={2}
                maxLength={3000}
                disabled={!!busy}
                onKeyDown={(event) => {
                  if (
                    event.key === "Enter" &&
                    !event.shiftKey &&
                    !event.nativeEvent.isComposing
                  ) {
                    event.preventDefault();
                    event.currentTarget.form?.requestSubmit();
                  }
                }}
              />
              <button
                className="send-button"
                type="submit"
                aria-label="Отправить сообщение"
                disabled={!input.trim() || !!busy}
              >
                <Arrow />
              </button>
            </form>
            <div className="composer-footnote">
              <span>Enter — отправить · Shift + Enter — новая строка</span>
              <span>Тестовые данные</span>
            </div>
          </section>

          <aside
            className="plan-panel"
            id="onboarding-plan"
            aria-label="План адаптации"
          >
            <div className="plan-heading">
              <div>
                <p className="eyebrow">План адаптации</p>
                <h2>Первые шаги</h2>
              </div>
              <span className="week-tag">
                {done} из {taskCount}
              </span>
            </div>
            <progress
              className="progress-track"
              aria-label="Прогресс адаптации"
              value={done}
              max={taskCount}
              aria-valuenow={done}
              aria-valuemin={0}
              aria-valuemax={taskCount}
            >
              {done}/{taskCount}
            </progress>
            <ol className="task-list">
              {plan?.tasks.map((task, index) => (
                <li key={task.id} className={`task-item task-${task.status}`}>
                  <span className="task-number" aria-hidden="true">
                    {task.status === "done"
                      ? "✓"
                      : String(index + 1).padStart(2, "0")}
                  </span>
                  <div className="task-body">
                    <h3>{task.title}</h3>
                    <p>{task.description}</p>
                    <span className={`status-label status-${task.status}`}>
                      {statusLabels[task.status] || task.status}
                    </span>
                    {task.manual &&
                      task.status !== "done" &&
                      task.status !== "blocked" && (
                        <button
                          className="text-button"
                          disabled={!!busy}
                          onClick={() => void complete(task.id)}
                        >
                          {busy === `task:${task.id}`
                            ? "Сохраняем…"
                            : task.id === "first-task"
                              ? "Задача выполнена"
                              : "Ознакомился"}
                        </button>
                      )}
                  </div>
                </li>
              ))}
            </ol>
            <div className="next-step">
              <div>
                <span className="mini-label">Следующий шаг</span>
                <p>{plan?.nextStep || "Загружаем план…"}</p>
              </div>
            </div>
            {!!requests.length && (
              <section className="requests-section" aria-label="Мои заявки">
                <h3>
                  Мои заявки <span>{requests.length}</span>
                </h3>
                {requests.map((request) => (
                  <article className="request-card" key={request.id}>
                    <div>
                      <strong>{request.resourceName}</strong>
                      <span className={`status-label status-${request.status}`}>
                        {statusLabels[request.status] || request.status}
                      </span>
                    </div>
                    <code>{request.id}</code>
                    <p>{request.message}</p>
                    <button
                      className="text-button"
                      disabled={!!busy}
                      onClick={() => void updateRequest(request)}
                    >
                      {busy === `request:${request.id}`
                        ? "Проверяем…"
                        : "Обновить статус"}
                    </button>
                    {config.demoMode && request.status === "pending" && (
                      <div className="simulation-controls">
                        <span>Имитация сервиса</span>
                        <div>
                          <button
                            disabled={!!busy}
                            onClick={() =>
                              void updateRequest(request, "granted")
                            }
                          >
                            Выдать доступ
                          </button>
                          <button
                            disabled={!!busy}
                            onClick={() =>
                              void updateRequest(request, "rejected")
                            }
                          >
                            Отклонить
                          </button>
                        </div>
                      </div>
                    )}
                  </article>
                ))}
              </section>
            )}
            <div className="mentor-note">
              <TopicIcon index={2} />
              <p>Сложный вопрос лучше обсудить с наставником своей команды.</p>
            </div>
          </aside>
        </main>
      )}
      {error && (
        <div className="notification error-notification" role="alert">
          <div>
            <strong>Не получилось завершить действие</strong>
            <p>{error}</p>
          </div>
          <button
            onClick={() =>
              user
                ? void loadWorkspace()
                    .then(() => setError(""))
                    .catch((failure) => setError(errorText(failure)))
                : void boot()
            }
            disabled={!!busy}
          >
            Повторить загрузку
          </button>
          <button
            className="close-button"
            aria-label="Закрыть ошибку"
            onClick={() => setError("")}
          >
            ×
          </button>
        </div>
      )}
      {notice && (
        <div className="notification success-notification" role="status">
          <p>{notice}</p>
          <button
            className="close-button"
            aria-label="Закрыть уведомление"
            onClick={() => setNotice("")}
          >
            ×
          </button>
        </div>
      )}
      <footer className="site-footer page-width">
        <span>Онбо · Hack&amp;Change 2026</span>
        <span>Тестовые профили, инструкции и сервисы</span>
      </footer>
    </div>
  );
}
