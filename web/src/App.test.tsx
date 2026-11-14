import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import { api, ApiError } from "./api";
import type { AccessRequest, ChatReply, Plan } from "./types";

vi.mock("./api", async (importOriginal) => {
  const original = await importOriginal<typeof import("./api")>();
  return {
    ...original,
    api: Object.fromEntries(
      Object.keys(original.api).map((key) => [key, vi.fn()]),
    ),
  };
});
const mocked = vi.mocked(api);
const users = [
  { id: "intern-platform", name: "Саша Тестов", team: "Платформа" },
  { id: "intern-analytics", name: "Маша Демо", team: "Аналитика" },
];
const initialPlan: Plan = {
  tasks: [
    {
      id: "team",
      title: "Познакомиться с командой",
      description: "Узнай, кто поможет",
      status: "todo",
      manual: true,
    },
    {
      id: "instructions",
      title: "Прочитать инструкцию",
      description: "Порядок работы",
      status: "todo",
      manual: true,
    },
    {
      id: "access",
      title: "Получить доступ",
      description: "Учебный репозиторий",
      status: "todo",
      manual: false,
    },
    {
      id: "first-task",
      title: "Первая задача",
      description: "Начни после выдачи доступа",
      status: "blocked",
      manual: true,
    },
  ],
  nextStep: "Познакомься с командой",
};
const draft = {
  id: "draft-1",
  resourceId: "training-repo",
  resourceName: "Учебный репозиторий",
  reason: "Подготовка к первой задаче",
};
const pending: AccessRequest = {
  id: "REQ-001",
  draftId: "draft-1",
  resourceId: "training-repo",
  resourceName: "Учебный репозиторий",
  status: "pending",
  message: "Заявка ожидает решения.",
};
const reply: ChatReply = {
  message: "Для первой задачи запроси доступ по тестовой инструкции.",
  sources: [
    {
      id: "doc-1",
      title: "Доступы команды",
      excerpt: "Для учебной задачи нужен учебный репозиторий.",
      label: "Тестовая инструкция",
    },
  ],
  draft,
};

beforeEach(() => {
  vi.clearAllMocks();
  mocked.profiles.mockResolvedValue(users);
  mocked.config.mockResolvedValue({ demoMode: true, llmMode: "demo" });
  mocked.session.mockResolvedValue(users[0]);
  mocked.login.mockResolvedValue(users[0]);
  mocked.plan.mockResolvedValue(initialPlan);
  mocked.messages.mockResolvedValue([]);
  mocked.requests.mockResolvedValue([]);
  mocked.chat.mockResolvedValue(reply);
  mocked.confirm.mockResolvedValue(pending);
  mocked.refresh.mockResolvedValue(pending);
  mocked.simulate.mockImplementation(async (_id, status) => ({
    ...pending,
    status,
  }));
  mocked.complete.mockResolvedValue({
    ...initialPlan,
    tasks: initialPlan.tasks.map((task) => ({
      ...task,
      status: task.id === "team" ? "done" : task.status,
    })),
  });
});

async function openApp() {
  const browser = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", { name: "На связи, Саша." });
  return browser;
}

describe("Onbo workspace", () => {
  it("opens a test profile, saves manual progress and keeps access blocked until granted", async () => {
    mocked.session.mockRejectedValue(new ApiError(401, "Сессия отсутствует"));
    const browser = userEvent.setup();
    render(<App />);
    expect(
      screen.getByText("Подготавливаем твоё рабочее пространство…"),
    ).toBeInTheDocument();
    await screen.findByRole("heading", { name: "Давай знакомиться" });
    expect(screen.getByText(/Это не регламенты Авито/)).toBeInTheDocument();
    await browser.click(
      screen.getByRole("button", { name: "Открыть мой план" }),
    );
    await screen.findByRole("heading", { name: "На связи, Саша." });
    await browser.click(
      screen.getAllByRole("button", { name: "Ознакомился" })[0],
    );
    await waitFor(() =>
      expect(screen.getByRole("progressbar")).toHaveAttribute(
        "aria-valuenow",
        "1",
      ),
    );
    expect(mocked.complete).toHaveBeenCalledWith("team");
    expect(
      screen.queryByRole("button", { name: "Задача выполнена" }),
    ).not.toBeInTheDocument();
  });

  it("passes from question to cited draft, explicit confirmation, granted access and the first task", async () => {
    const browser = await openApp();
    await browser.click(
      screen.getByRole("button", {
        name: "Как получить доступ к репозиторию?",
      }),
    );
    expect(screen.getByLabelText("Сообщение Онбо")).toHaveValue(
      "Как получить доступ к репозиторию?",
    );
    await browser.click(
      screen.getByRole("button", { name: "Отправить сообщение" }),
    );
    await screen.findByText(reply.message);
    expect(screen.getByText("Доступы команды")).toBeInTheDocument();
    expect(mocked.confirm).not.toHaveBeenCalled();
    await browser.click(
      screen.getByRole("button", { name: "Подтвердить и отправить" }),
    );
    await screen.findByText("REQ-001");
    expect(mocked.confirm).toHaveBeenCalledWith("draft-1");
    expect(
      screen.getByRole("button", { name: "Заявка отправлена" }),
    ).toBeDisabled();
    await browser.click(
      screen.getByRole("button", { name: "Закрыть уведомление" }),
    );
    const grantedPlan = {
      ...initialPlan,
      tasks: initialPlan.tasks.map((task) => ({
        ...task,
        status:
          task.id === "access"
            ? "done"
            : task.id === "first-task"
              ? "todo"
              : task.status,
      })),
    };
    mocked.plan.mockResolvedValue(grantedPlan);
    await browser.click(screen.getByRole("button", { name: "Выдать доступ" }));
    await screen.findByText("Доступ выдан");
    expect(mocked.simulate).toHaveBeenCalledWith("REQ-001", "granted");
    mocked.complete.mockResolvedValue({
      ...grantedPlan,
      tasks: grantedPlan.tasks.map((task) => ({
        ...task,
        status: task.id === "first-task" ? "done" : task.status,
      })),
    });
    await browser.click(
      screen.getByRole("button", { name: "Задача выполнена" }),
    );
    await waitFor(() =>
      expect(mocked.complete).toHaveBeenLastCalledWith("first-task"),
    );
  });

  it("preserves an unsent message when GigaChat fails and allows retry", async () => {
    mocked.chat.mockRejectedValueOnce(new ApiError(502, "Ошибка GigaChat"));
    const browser = await openApp();
    await browser.type(
      screen.getByLabelText("Сообщение Онбо"),
      "Где инструкция?",
    );
    await browser.click(
      screen.getByRole("button", { name: "Отправить сообщение" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "сейчас не отвечает",
    );
    expect(screen.getByLabelText("Сообщение Онбо")).toHaveValue(
      "Где инструкция?",
    );
    expect(screen.queryByText(reply.message)).not.toBeInTheDocument();
    await browser.click(screen.getByRole("button", { name: "Закрыть ошибку" }));
    await browser.click(
      screen.getByRole("button", { name: "Отправить сообщение" }),
    );
    await screen.findByText(reply.message);
    expect(screen.getByLabelText("Сообщение Онбо")).toHaveValue("");
  });

  it("never reports a successful submission on a forbidden confirmation", async () => {
    mocked.messages.mockResolvedValue([
      { role: "assistant", text: reply.message, draft },
    ]);
    mocked.confirm.mockRejectedValue(new ApiError(403, "Нет прав на ресурс"));
    const browser = await openApp();
    await browser.click(
      screen.getByRole("button", { name: "Подтвердить и отправить" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Действие недоступно",
    );
    expect(screen.queryByText("REQ-001")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Заявка отправлена" }),
    ).not.toBeInTheDocument();
  });

  it("shows uncertain submission separately and restores the same operation", async () => {
    mocked.messages.mockResolvedValue([
      { role: "assistant", text: "Проверь черновик", draft },
    ]);
    mocked.confirm
      .mockResolvedValueOnce({
        ...pending,
        id: "operation-local",
        status: "submitting",
        message: "Исход запроса неизвестен",
      })
      .mockResolvedValueOnce(pending);
    const browser = await openApp();
    await browser.click(
      screen.getByRole("button", { name: "Подтвердить и отправить" }),
    );
    await screen.findByText("Результат уточняется");
    expect(
      screen.getByText(/Ответ сервиса пока неизвестен/),
    ).toBeInTheDocument();
    await browser.click(
      screen.getByRole("button", { name: "Восстановить отправку" }),
    );
    await screen.findByRole("button", { name: "Заявка отправлена" });
    expect(mocked.confirm.mock.calls).toEqual([["draft-1"], ["draft-1"]]);
    expect(screen.getAllByText("REQ-001")).toHaveLength(1);
    expect(screen.queryByText("operation-local")).not.toBeInTheDocument();
    expect(
      within(screen.getByLabelText("Мои заявки")).getAllByRole("article"),
    ).toHaveLength(1);
  });

  it("replaces a local operation with the recovered external request on refresh", async () => {
    mocked.messages.mockResolvedValue([
      { role: "assistant", text: "Проверь черновик", draft },
    ]);
    mocked.requests.mockResolvedValue([
      {
        ...pending,
        id: "operation-local",
        status: "submitting",
        message: "Исход запроса неизвестен",
      },
    ]);
    mocked.refresh.mockResolvedValue(pending);
    const browser = await openApp();
    expect(screen.getByText("operation-local")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Восстановить отправку" }),
    ).toBeInTheDocument();
    await browser.click(
      screen.getByRole("button", { name: "Обновить статус" }),
    );
    await screen.findByText("REQ-001");
    expect(mocked.refresh).toHaveBeenCalledWith("operation-local");
    expect(screen.queryByText("operation-local")).not.toBeInTheDocument();
    expect(screen.queryByText("Результат уточняется")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Заявка отправлена" }),
    ).toBeDisabled();
    expect(
      within(screen.getByLabelText("Мои заявки")).getAllByRole("article"),
    ).toHaveLength(1);
  });

  it("loads persisted history, refreshes status and supports a simulated rejection", async () => {
    mocked.messages.mockResolvedValue([
      { role: "user", text: "Мой вопрос" },
      {
        role: "assistant",
        text: "Сохранённый ответ",
        sources: [
          { id: "doc2", title: "Инструкция", excerpt: "Фрагмент", label: "" },
        ],
        draft,
      },
    ]);
    mocked.requests.mockResolvedValue([pending]);
    const browser = await openApp();
    expect(screen.getByText("Мой вопрос")).toBeInTheDocument();
    expect(screen.getByText("Тестовая инструкция")).toBeInTheDocument();
    await browser.click(
      screen.getByRole("button", { name: "Обновить статус" }),
    );
    await waitFor(() => expect(mocked.refresh).toHaveBeenCalledWith("REQ-001"));
    await browser.click(screen.getByRole("button", { name: "Отклонить" }));
    await screen.findByText("Нужна помощь");
    expect(mocked.simulate).toHaveBeenCalledWith("REQ-001", "rejected");
  });

  it("switches profiles without retaining the previous history and hides simulation controls outside demo mode", async () => {
    mocked.config.mockResolvedValue({ demoMode: false, llmMode: "gigachat" });
    mocked.messages
      .mockResolvedValueOnce([{ role: "assistant", text: "Только Саше" }])
      .mockResolvedValue([]);
    mocked.requests.mockResolvedValueOnce([pending]).mockResolvedValue([]);
    const browser = await openApp();
    expect(screen.getByText("GigaChat")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Выдать доступ" }),
    ).not.toBeInTheDocument();
    mocked.login.mockResolvedValue(users[1]);
    await browser.selectOptions(
      screen.getByLabelText("Тестовый профиль"),
      "intern-analytics",
    );
    await screen.findByRole("heading", { name: "На связи, Маша." });
    expect(screen.queryByText("Только Саше")).not.toBeInTheDocument();
    expect(screen.queryByText("REQ-001")).not.toBeInTheDocument();
  });

  it("reports workspace load failures and recovers with the reload action", async () => {
    mocked.plan.mockRejectedValueOnce(new Error("База недоступна"));
    const browser = await openApp();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "База недоступна",
    );
    await browser.click(
      screen.getByRole("button", { name: "Повторить загрузку" }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
    );
    expect(screen.getByText("Познакомиться с командой")).toBeInTheDocument();
  });

  it("recovers boot failures, and rejects invalid sign-in without showing a workspace", async () => {
    mocked.profiles.mockRejectedValueOnce(new Error("Нет соединения"));
    mocked.session.mockRejectedValue(new ApiError(401, "Нет сессии"));
    const browser = userEvent.setup();
    render(<App />);
    await screen.findByRole("alert");
    await browser.click(
      screen.getByRole("button", { name: "Повторить загрузку" }),
    );
    await screen.findByRole("heading", { name: "Давай знакомиться" });
    await browser.selectOptions(
      screen.getByLabelText("Тестовый профиль"),
      "intern-analytics",
    );
    mocked.login.mockRejectedValueOnce(new Error("Профиль недоступен"));
    await browser.click(
      screen.getByRole("button", { name: "Открыть мой план" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Профиль недоступен",
    );
    expect(
      screen.queryByRole("heading", { name: "На связи, Маша." }),
    ).not.toBeInTheDocument();
  });

  it("shows an in-flight model state, blocks duplicates, supports Enter and leaves Shift+Enter in the composer", async () => {
    let resolve!: (value: ChatReply) => void;
    mocked.chat.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const browser = await openApp();
    const input = screen.getByLabelText("Сообщение Онбо");
    fireEvent.change(input, { target: { value: "Вопрос" } });
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
    expect(mocked.chat).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: "Enter" });
    await screen.findByText("Онбо разбирается в вопросе…");
    expect(
      screen.getByRole("button", { name: "Отправить сообщение" }),
    ).toBeDisabled();
    await act(async () => resolve({ message: "Готово", sources: [] }));
    await screen.findByText("Готово");
    expect(mocked.chat).toHaveBeenCalledTimes(1);
    await browser.click(screen.getByLabelText("Сообщение Онбо"));
  });

  it("keeps server state when a manual task or status update fails", async () => {
    mocked.requests.mockResolvedValue([pending]);
    mocked.complete.mockRejectedValueOnce(new Error("Не сохранено"));
    mocked.refresh.mockRejectedValueOnce(new Error("Нет ответа сервиса"));
    const browser = await openApp();
    await browser.click(
      screen.getAllByRole("button", { name: "Ознакомился" })[0],
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("Не сохранено");
    expect(screen.getByRole("progressbar")).toHaveAttribute(
      "aria-valuenow",
      "0",
    );
    await browser.click(
      screen.getByRole("button", { name: "Обновить статус" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Нет ответа сервиса",
    );
    expect(
      within(screen.getByLabelText("Мои заявки")).getByText(
        "Ожидает согласования",
      ),
    ).toBeInTheDocument();
  });
});
