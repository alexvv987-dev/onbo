import type {
  AccessRequest,
  ChatReply,
  Config,
  Message,
  Plan,
  User,
} from "./types";

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export async function request<T>(
  path: string,
  body?: unknown,
  method = "GET",
): Promise<T> {
  const response = await fetch(`/api${path}`, {
    method,
    credentials: "same-origin",
    headers:
      body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    const payload: { error?: string } = await response.json().catch(() => ({}));
    throw new ApiError(
      response.status,
      payload.error || `Сервис вернул ошибку ${response.status}`,
    );
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export const api = {
  config: () => request<Config>("/config"),
  profiles: () => request<User[]>("/profiles"),
  session: () => request<User>("/session"),
  login: (profileId: string) =>
    request<User>("/session", { profileId }, "POST"),
  logout: () => request<void>("/session", undefined, "DELETE"),
  plan: () => request<Plan>("/plan"),
  complete: (id: string) =>
    request<Plan>(`/tasks/${encodeURIComponent(id)}/complete`, {}, "POST"),
  messages: () => request<Message[]>("/messages"),
  chat: (message: string) => request<ChatReply>("/chat", { message }, "POST"),
  requests: () => request<AccessRequest[]>("/requests"),
  confirm: (id: string) =>
    request<AccessRequest>(
      `/drafts/${encodeURIComponent(id)}/confirm`,
      { confirmed: true },
      "POST",
    ),
  refresh: (id: string) =>
    request<AccessRequest>(
      `/requests/${encodeURIComponent(id)}/refresh`,
      {},
      "POST",
    ),
  simulate: (id: string, status: "granted" | "rejected") =>
    request<AccessRequest>(
      `/demo/requests/${encodeURIComponent(id)}/status`,
      { status },
      "POST",
    ),
};

export function errorText(error: unknown): string {
  if (error instanceof ApiError && error.status === 403)
    return `Действие недоступно для этого профиля. ${error.message}`;
  if (error instanceof ApiError && [502, 504].includes(error.status))
    return `Помощник или внешний сервис сейчас не отвечает. ${error.message}`;
  return error instanceof Error
    ? error.message
    : "Не удалось выполнить действие. Попробуйте ещё раз.";
}
