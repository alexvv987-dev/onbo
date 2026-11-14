import { afterEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, errorText, request } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("HTTP boundary", () => {
  it("sends session credentials and explicit confirmation, without user identity", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ id: "request-1" }), { status: 200 }),
      );
    vi.stubGlobal("fetch", fetcher);
    await api.confirm("draft/1");
    expect(fetcher).toHaveBeenCalledWith(
      "/api/drafts/draft%2F1/confirm",
      expect.objectContaining({
        credentials: "same-origin",
        method: "POST",
        body: JSON.stringify({ confirmed: true }),
      }),
    );
    expect(fetcher.mock.calls[0][1].body).not.toContain("user");
  });

  it("preserves server errors, including invalid JSON and non-Error failures", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(
          new Response(JSON.stringify({ error: "Нет прав" }), { status: 403 }),
        )
        .mockResolvedValueOnce(new Response("unavailable", { status: 502 })),
    );
    await expect(request("/plan")).rejects.toEqual(
      new ApiError(403, "Нет прав"),
    );
    await expect(request("/plan")).rejects.toThrow("Сервис вернул ошибку 502");
    expect(errorText(new ApiError(403, "Чужая заявка"))).toContain(
      "Действие недоступно",
    );
    expect(errorText(new ApiError(502, "Модель недоступна"))).toContain(
      "сейчас не отвечает",
    );
    expect(errorText(new ApiError(504, "Таймаут"))).toContain(
      "сейчас не отвечает",
    );
    expect(errorText(new Error("Сеть"))).toBe("Сеть");
    expect(errorText(null)).toContain("Попробуйте ещё раз");
  });

  it("supports empty responses and the complete contracted resource operations", async () => {
    const fetcher = vi
      .fn()
      .mockImplementation((_path: string, options: RequestInit) =>
        Promise.resolve(
          options.method === "DELETE"
            ? new Response(null, { status: 204 })
            : new Response(JSON.stringify([]), { status: 200 }),
        ),
      );
    vi.stubGlobal("fetch", fetcher);
    await api.config();
    await api.profiles();
    await api.session();
    await api.login("intern-platform");
    await api.plan();
    await api.complete("team");
    await api.messages();
    await api.chat("Привет");
    await api.requests();
    await api.refresh("req/1");
    await api.simulate("req/1", "granted");
    await expect(api.logout()).resolves.toBeUndefined();
    expect(fetcher.mock.calls.map((call) => call[0])).toEqual([
      "/api/config",
      "/api/profiles",
      "/api/session",
      "/api/session",
      "/api/plan",
      "/api/tasks/team/complete",
      "/api/messages",
      "/api/chat",
      "/api/requests",
      "/api/requests/req%2F1/refresh",
      "/api/demo/requests/req%2F1/status",
      "/api/session",
    ]);
    expect(fetcher.mock.calls[3][1].body).toBe(
      JSON.stringify({ profileId: "intern-platform" }),
    );
    expect(fetcher.mock.calls[7][1].body).toBe(
      JSON.stringify({ message: "Привет" }),
    );
    expect(fetcher.mock.calls[10][1].body).toBe(
      JSON.stringify({ status: "granted" }),
    );
  });
});
