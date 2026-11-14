package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"onbo/internal/config"
	"onbo/internal/core"
	mockservice "onbo/internal/mock"
)

type testAgent struct {
	reply core.ChatReply
	err   error
}

type waitingAgent struct{}

func (waitingAgent) GenerateReply(ctx context.Context, _ core.User, _ []core.Message, _ string) (core.ChatReply, error) {
	<-ctx.Done()
	return core.ChatReply{}, ctx.Err()
}

func TestConfiguredSessionsAndRequestLimits(test *testing.T) {
	store, sessions, _, _ := setup(test)
	settings := config.DefaultApp()
	settings.Redis.SessionTTL = 30 * time.Minute
	settings.API.MaxRequestBytes = 64
	settings.API.ChatTimeout = 500 * time.Millisecond
	handler := NewServer(store, sessions, waitingAgent{}, Config{DemoMode: true, SessionTTL: settings.Redis.SessionTTL, API: settings.API})
	cookie := login(test, handler, "intern-platform")
	if cookie.MaxAge != 1800 {
		test.Fatalf("cookie TTL: %d", cookie.MaxAge)
	}
	oversizedBody := `{"message":"` + strings.Repeat("a", 100) + `"}`
	expect(test, call(test, handler, "POST", "/api/chat", oversizedBody, cookie), 400)
	expect(test, call(test, handler, "POST", "/api/chat", `{"message":"hello"}`, cookie), 504)
	accessSettings := settings.Mock
	accessSettings.Timeout = 17 * time.Millisecond
	configuredStore, err := NewStore(context.Background(), testDB(test), "http://localhost:1", "token", accessSettings)
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(configuredStore.Close)
	if configuredStore.Client.Timeout != 17*time.Millisecond {
		test.Fatal("mock client timeout ignored")
	}
}

func TestConfiguredRedisTTLAndQuota(test *testing.T) {
	address := os.Getenv("ONBO_TEST_REDIS_ADDR")
	if address == "" {
		test.Skip("Redis integration configuration required")
	}
	settings := config.DefaultRedis()
	settings.SessionTTL = 30 * time.Minute
	settings.RateWindow = 1500 * time.Millisecond
	settings.ChatRateLimit = 2
	sessions := NewRedisSessions(address, "", settings)
	test.Cleanup(func() { _ = sessions.Close() })
	userID := generateIdentifier()
	ctx := context.Background()
	token, err := sessions.CreateSession(ctx, userID)
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() {
		_ = sessions.DeleteSession(ctx, token)
		_ = sessions.Client.Del(ctx, chatLimitKeyPrefix+userID).Err()
	})
	ttl, err := sessions.Client.TTL(ctx, sessionKeyPrefix+token).Result()
	if err != nil || ttl <= 29*time.Minute || ttl > 30*time.Minute {
		test.Fatalf("session TTL ignored: %v %v", ttl, err)
	}
	for messageIndex := 0; messageIndex < 3; messageIndex++ {
		allowed, err := sessions.ConsumeChatQuota(ctx, userID)
		if err != nil || allowed != (messageIndex < 2) {
			test.Fatalf("quota ignored: %v %v", allowed, err)
		}
	}
	window, err := sessions.Client.PTTL(ctx, chatLimitKeyPrefix+userID).Result()
	if err != nil || window <= 0 || window > 1500*time.Millisecond {
		test.Fatalf("quota window ignored: %v %v", window, err)
	}
}

func (testAgent testAgent) GenerateReply(context.Context, core.User, []core.Message, string) (core.ChatReply, error) {
	return testAgent.reply, testAgent.err
}

type fakeSessions struct {
	users   map[string]string
	fail    bool
	limited bool
}

func (sessions *fakeSessions) CreateSession(_ context.Context, user string) (string, error) {
	if sessions.fail {
		return "", ErrUnavailable
	}
	if sessions.users == nil {
		sessions.users = map[string]string{}
	}
	identifier := generateIdentifier()
	sessions.users[identifier] = user
	return identifier, nil
}
func (sessions *fakeSessions) GetSessionUserID(_ context.Context, responseValue string) (string, error) {
	if sessions.fail {
		return "", ErrUnavailable
	}
	if user, ok := sessions.users[responseValue]; ok {
		return user, nil
	}
	return "", ErrForbidden
}
func (sessions *fakeSessions) DeleteSession(_ context.Context, responseValue string) error {
	if sessions.fail {
		return ErrUnavailable
	}
	delete(sessions.users, responseValue)
	return nil
}
func (sessions *fakeSessions) ConsumeChatQuota(context.Context, string) (bool, error) {
	return !sessions.limited, func() error {
		if sessions.fail {
			return ErrUnavailable
		}
		return nil
	}()
}
func (sessions *fakeSessions) Ping(context.Context) error {
	if sessions.fail {
		return ErrUnavailable
	}
	return nil
}

func testDB(test *testing.T) string {
	test.Helper()
	raw := os.Getenv("ONBO_TEST_DATABASE_URL")
	if raw == "" {
		test.Skip("ONBO_TEST_DATABASE_URL is required for PostgreSQL integration")
	}
	ctx := context.Background()
	databasePool, err := pgxpool.New(ctx, raw)
	if err != nil {
		test.Fatal(err)
	}
	schema := "onbo_test_" + generateIdentifier()
	_, err = databasePool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() {
		_, _ = databasePool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		databasePool.Close()
	})
	separator := "?"
	if strings.Contains(raw, "?") {
		separator = "&"
	}
	return raw + separator + "search_path=" + schema
}
func setup(test *testing.T) (*Store, *fakeSessions, http.Handler, string) {
	test.Helper()
	db := testDB(test)
	mockServer, err := mockservice.New(context.Background(), db, "test-token")
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(mockServer.Close)
	remote := httptest.NewServer(mockServer)
	test.Cleanup(remote.Close)
	store, err := NewStore(context.Background(), db, remote.URL, "test-token")
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(store.Close)
	sessions := &fakeSessions{}
	testAgent := NewServer(store, sessions, testAgent{reply: core.ChatReply{Message: "Тестовый ответ", Sources: []core.Source{{ID: "workflow", Title: "Инструкция", Excerpt: "Фрагмент", Label: "Тестовая"}}}}, Config{DemoMode: true, LLMMode: "demo"})
	return store, sessions, testAgent, db
}
func call(test *testing.T, handler http.Handler, method, path, body string, cookie *http.Cookie, headers ...string) *httptest.ResponseRecorder {
	test.Helper()
	request := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
	if cookie != nil {
		request.AddCookie(cookie)
	}
	for itemIndex := 0; itemIndex < len(headers); itemIndex += 2 {
		request.Header.Set(headers[itemIndex], headers[itemIndex+1])
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
func expect(test *testing.T, recorder *httptest.ResponseRecorder, status int) {
	test.Helper()
	if recorder.Code != status {
		test.Fatalf("status=%d want=%d body=%s", recorder.Code, status, recorder.Body.String())
	}
}
func decodeResult[Result any](test *testing.T, recorder *httptest.ResponseRecorder) Result {
	test.Helper()
	var responseValue Result
	if err := json.Unmarshal(recorder.Body.Bytes(), &responseValue); err != nil {
		test.Fatal(err)
	}
	return responseValue
}
func login(test *testing.T, handler http.Handler, userID string) *http.Cookie {
	test.Helper()
	recorder := call(test, handler, "POST", "/api/session", `{"profileId":"`+userID+`"}`, nil)
	expect(test, recorder, 200)
	cs := recorder.Result().Cookies()
	if len(cs) != 1 || !cs[0].HttpOnly || cs[0].SameSite != http.SameSiteLaxMode {
		test.Fatal("unsafe session cookie")
	}
	return cs[0]
}

func TestJourneyPermissionsAndPersistence(test *testing.T) {
	service, _, handler, db := setup(test)
	ctx := context.Background()
	user, _ := FindProfile("intern-platform")
	other, _ := FindProfile("intern-analytics")
	expect(test, call(test, handler, "GET", "/healthz", "", nil), 200)
	expect(test, call(test, handler, "GET", "/api/config", "", nil), 200)
	expect(test, call(test, handler, "GET", "/api/profiles", "", nil), 200)
	expect(test, call(test, handler, "GET", "/api/plan", "", nil), 401)
	for _, body := range []string{`{`, `{"profileId":"bad"}`, `{"profileId":"intern-platform","userId":"intruder"}`, `{"profileId":"intern-platform"} {}`} {
		expect(test, call(test, handler, "POST", "/api/session", body, nil), 400)
	}
	cookie := login(test, handler, user.ID)
	c2 := login(test, handler, other.ID)
	expect(test, call(test, handler, "GET", "/api/session", "", cookie), 200)
	onboardingPlan := decodeResult[core.Plan](test, call(test, handler, "GET", "/api/plan", "", cookie))
	if len(onboardingPlan.Tasks) != 4 || onboardingPlan.Tasks[3].Status != "blocked" {
		test.Fatal(onboardingPlan)
	}
	expect(test, call(test, handler, "POST", "/api/tasks/access/complete", `{}`, cookie), 403)
	expect(test, call(test, handler, "POST", "/api/tasks/first-task/complete", `{}`, cookie), 403)
	expect(test, call(test, handler, "POST", "/api/tasks/missing/complete", `{}`, cookie), 404)
	for _, step := range []string{"team", "instructions"} {
		expect(test, call(test, handler, "POST", "/api/tasks/"+step+"/complete", `{}`, cookie), 200)
	}
	if resources, err := service.ListAccessResources(ctx, other); err != nil || len(resources) != 0 {
		test.Fatal(resources, err)
	}
	for _, query := range []string{"доступ", "первая задача", "репозиторий"} {
		sources, err := service.SearchKnowledge(ctx, user, query)
		if err != nil || len(sources) == 0 {
			test.Fatal(query, sources, err)
		}
		for _, accessDraft := range sources {
			if strings.Contains(accessDraft.Excerpt, "SECRET") {
				test.Fatal("cross-team document leaked")
			}
		}
	}
	sources, err := service.SearchKnowledge(ctx, user, "ALPHA-ANALYTICS-SECRET")
	if err != nil || len(sources) != 0 {
		test.Fatal(sources, err)
	}
	if _, err = service.SearchKnowledge(ctx, user, ""); !errors.Is(err, ErrInvalid) {
		test.Fatal(err)
	}
	if _, err = service.SearchKnowledge(ctx, core.User{ID: "unknown"}, "доступ"); !errors.Is(err, ErrForbidden) {
		test.Fatal(err)
	}
	expect(test, call(test, handler, "POST", "/api/drafts", `{"resourceId":"training-repo","reason":""}`, cookie), 400)
	expect(test, call(test, handler, "POST", "/api/drafts", `{"resourceId":"training-repo","reason":"Нужно для задачи"}`, c2), 403)
	accessDraft := decodeResult[core.Draft](test, call(test, handler, "POST", "/api/drafts", `{"resourceId":"training-repo","reason":"Подготовка к первой задаче"}`, cookie))
	expect(test, call(test, handler, "POST", "/api/drafts/"+accessDraft.ID+"/confirm", `{"confirmed":false}`, cookie), 400)
	expect(test, call(test, handler, "POST", "/api/drafts/"+accessDraft.ID+"/confirm", `{"confirmed":true}`, c2), 404)
	recorder := call(test, handler, "POST", "/api/drafts/"+accessDraft.ID+"/confirm", `{"confirmed":true}`, cookie)
	expect(test, recorder, 200)
	request := decodeResult[core.AccessRequest](test, recorder)
	if request.Status != "pending" || !strings.HasPrefix(request.ID, "REQ-") {
		test.Fatal(request)
	}
	again := decodeResult[core.AccessRequest](test, call(test, handler, "POST", "/api/drafts/"+accessDraft.ID+"/confirm", `{"confirmed":true}`, cookie))
	if again.ID != request.ID {
		test.Fatal("duplicate created")
	}
	expect(test, call(test, handler, "POST", "/api/requests/"+request.ID+"/refresh", `{}`, c2), 404)
	expect(test, call(test, handler, "POST", "/api/requests/"+request.ID+"/refresh", `{}`, cookie), 200)
	onboardingPlan, _ = service.GetOnboardingPlan(ctx, user)
	if onboardingPlan.Tasks[2].Status != "pending" || onboardingPlan.Tasks[3].Status != "blocked" {
		test.Fatal(onboardingPlan)
	}
	expect(test, call(test, handler, "POST", "/api/demo/requests/"+request.ID+"/status", `{"status":"bogus"}`, cookie), 400)
	expect(test, call(test, handler, "POST", "/api/demo/requests/"+request.ID+"/status", `{"status":"granted"}`, cookie), 200)
	onboardingPlan, _ = service.GetOnboardingPlan(ctx, user)
	if onboardingPlan.Tasks[2].Status != "done" || onboardingPlan.Tasks[3].Status != "todo" {
		test.Fatal(onboardingPlan)
	}
	expect(test, call(test, handler, "POST", "/api/tasks/first-task/complete", `{}`, cookie), 200)
	onboardingPlan, _ = service.GetOnboardingPlan(ctx, user)
	for _, task := range onboardingPlan.Tasks {
		if task.Status != "done" {
			test.Fatal(onboardingPlan)
		}
	}
	reopened, err := NewStore(ctx, db, service.MockURL, "test-token")
	if err != nil {
		test.Fatal(err)
	}
	defer reopened.Close()
	restored, err := reopened.GetOnboardingPlan(ctx, user)
	if err != nil || restored.Tasks[0].Status != "done" || restored.Tasks[2].Status != "done" {
		test.Fatal(restored, err)
	}
	otherPlan, _ := service.GetOnboardingPlan(ctx, other)
	if otherPlan.Tasks[0].Status != "todo" || otherPlan.Tasks[2].Status != "blocked" {
		test.Fatal(otherPlan)
	}
	expect(test, call(test, handler, "POST", "/api/chat", `{"message":"Как подготовиться?"}`, cookie), 200)
	messages, err := service.ListChatMessages(ctx, user)
	if err != nil || len(messages) != 2 || messages[1].Role != "assistant" {
		test.Fatal(messages, err)
	}
	expect(test, call(test, handler, "GET", "/api/messages", "", cookie), 200)
	otherMessages, _ := service.ListChatMessages(ctx, other)
	if len(otherMessages) != 0 {
		test.Fatal("history leaked")
	}
	expect(test, call(test, handler, "GET", "/api/requests", "", cookie), 200)
	expect(test, call(test, handler, "GET", "/api/resources", "", cookie), 200)
	expect(test, call(test, handler, "DELETE", "/api/session", "", cookie), 204)
	expect(test, call(test, handler, "GET", "/api/session", "", cookie), 401)
}

func TestRecoveryAfterAcceptedTimeoutAndConcurrentConfirmation(test *testing.T) {
	service, _, _, _ := setup(test)
	user, _ := FindProfile("intern-platform")
	ctx := context.Background()
	accessDraft, err := service.CreateAccessDraft(ctx, user, "training-repo", "Подготовка окружения")
	if err != nil {
		test.Fatal(err)
	}
	target := service.MockURL
	var once sync.Once
	intercept := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		forwardedRequest, _ := http.NewRequestWithContext(request.Context(), request.Method, target+request.URL.Path, request.Body)
		forwardedRequest.Header = request.Header.Clone()
		response, err := http.DefaultClient.Do(forwardedRequest)
		if err != nil {
			http.Error(responseWriter, "remote", 502)
			return
		}
		defer response.Body.Close()
		var raw bytes.Buffer
		_, _ = raw.ReadFrom(response.Body)
		if request.Method == "POST" && request.URL.Path == "/requests" {
			once.Do(func() { time.Sleep(100 * time.Millisecond) })
		}
		responseWriter.WriteHeader(response.StatusCode)
		_, _ = responseWriter.Write(raw.Bytes())
	}))
	defer intercept.Close()
	service.MockURL = intercept.URL
	service.Client = &http.Client{Timeout: 25 * time.Millisecond}
	request, err := service.ConfirmAndSubmitAccessDraft(ctx, user, accessDraft.ID, true)
	if !errors.Is(err, ErrUnavailable) || request.Status != "submitting" {
		test.Fatal(request, err)
	}
	service.Client = &http.Client{Timeout: time.Second}
	resolved, err := service.ConfirmAndSubmitAccessDraft(ctx, user, accessDraft.ID, true)
	if err != nil || resolved.Status != "pending" {
		test.Fatal(resolved, err)
	}
	var wg sync.WaitGroup
	for itemIndex := 0; itemIndex < 8; itemIndex++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request, err := service.ConfirmAndSubmitAccessDraft(ctx, user, accessDraft.ID, true)
			if err != nil || request.ID != resolved.ID {
				test.Errorf("concurrent result %v %v", request, err)
			}
		}()
	}
	wg.Wait()
	all, err := service.ListAccessRequests(ctx, user)
	if err != nil || len(all) != 1 {
		test.Fatal(all, err)
	}
	declined, err := service.SimulateAccessRequestStatus(ctx, user, resolved.ID, "rejected")
	if err != nil || declined.Status != "rejected" {
		test.Fatal(declined, err)
	}
	onboardingPlan, err := service.GetOnboardingPlan(ctx, user)
	if err != nil || onboardingPlan.Tasks[2].Status != "rejected" {
		test.Fatal(onboardingPlan, err)
	}
}

func TestDelayedPendingCannotOverwriteTerminalOutcome(test *testing.T) {
	for _, terminal := range []string{"granted", "rejected"} {
		test.Run(terminal, func(test *testing.T) {
			service, _, _, _ := setup(test)
			ctx := context.Background()
			user, _ := FindProfile("intern-platform")
			accessDraft, err := service.CreateAccessDraft(ctx, user, "training-repo", "Первая задача")
			if err != nil {
				test.Fatal(err)
			}
			pending, err := service.ConfirmAndSubmitAccessDraft(ctx, user, accessDraft.ID, true)
			if err != nil {
				test.Fatal(err)
			}
			target := service.MockURL
			captured := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			proxy := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
				forward, _ := http.NewRequestWithContext(request.Context(), request.Method, target+request.URL.Path, request.Body)
				forward.Header = request.Header.Clone()
				response, err := http.DefaultClient.Do(forward)
				if err != nil {
					http.Error(responseWriter, "remote unavailable", 502)
					return
				}
				defer response.Body.Close()
				var body bytes.Buffer
				_, _ = body.ReadFrom(response.Body)
				if request.Method == http.MethodGet {
					once.Do(func() { close(captured); <-release })
				}
				responseWriter.WriteHeader(response.StatusCode)
				_, _ = responseWriter.Write(body.Bytes())
			}))
			defer proxy.Close()
			service.MockURL = proxy.URL
			type outcome struct {
				request core.AccessRequest
				err     error
			}
			result := make(chan outcome, 1)
			go func() {
				responseValue, err := service.RefreshAccessRequestStatus(ctx, user, pending.ID)
				result <- outcome{responseValue, err}
			}()
			select {
			case <-captured:
			case <-time.After(2 * time.Second):
				close(release)
				test.Fatal("pending snapshot was not captured")
			}
			final, err := service.SimulateAccessRequestStatus(ctx, user, pending.ID, terminal)
			close(release)
			if err != nil {
				test.Fatal(err)
			}
			got := <-result
			if got.err != nil || got.request.Status != terminal || got.request.Message != final.Message {
				test.Fatalf("delayed pending overwrote terminal outcome: %+v, %v", got.request, got.err)
			}
			stored, err := service.ListAccessRequests(ctx, user)
			if err != nil || len(stored) != 1 || stored[0].Status != terminal {
				test.Fatal(stored, err)
			}
			plan, err := service.GetOnboardingPlan(ctx, user)
			if err != nil {
				test.Fatal(err)
			}
			want := terminal
			if terminal == "granted" {
				want = "done"
			}
			if plan.Tasks[2].Status != want {
				test.Fatal(plan)
			}
		})
	}
}

func TestHTTPGuardsFailuresAndStatic(test *testing.T) {
	service, sessions, handler, _ := setup(test)
	cookie := login(test, handler, "intern-platform")
	for _, headers := range [][]string{{"Origin", "http://evil.test"}, {"Origin", "https://localhost"}, {"Origin", "http://localhost/path"}, {"Origin", "null"}, {"Sec-Fetch-Site", "cross-site"}} {
		expect(test, call(test, handler, "POST", "/api/tasks/team/complete", `{}`, cookie, headers...), 403)
	}
	expect(test, call(test, handler, "POST", "/api/tasks/team/complete", `{}`, cookie, "Origin", "http://localhost"), 200)
	expect(test, call(test, handler, "POST", "/api/chat", `{"message":" "}`, cookie), 400)
	expect(test, call(test, handler, "POST", "/api/chat", `{"message":"`+strings.Repeat("я", 3001)+`"}`, cookie), 400)
	expect(test, call(test, handler, "POST", "/api/drafts", `{} {}`, cookie), 400)
	expect(test, call(test, handler, "POST", "/api/drafts", strings.Repeat(" ", 33000), cookie), 400)
	expect(test, call(test, handler, "GET", "/api/unknown", "", cookie), 404)
	expect(test, call(test, handler, "POST", "/not-an-api", `{}`, cookie), 405)
	expect(test, call(test, handler, "GET", "/", "", nil), 404)
	sessions.limited = true
	expect(test, call(test, handler, "POST", "/api/chat", `{"message":"hello"}`, cookie), 429)
	sessions.limited = false
	for _, item := range []struct {
		err    error
		status int
	}{{errors.New("model failed"), 502}, {context.DeadlineExceeded, 504}} {
		bad := NewServer(service, sessions, testAgent{err: item.err}, Config{DemoMode: true})
		expect(test, call(test, bad, "POST", "/api/chat", `{"message":"hello"}`, cookie), item.status)
	}
	disabled := NewServer(service, sessions, testAgent{}, Config{})
	expect(test, call(test, disabled, "GET", "/api/profiles", "", nil), 403)
	expect(test, call(test, disabled, "POST", "/api/session", `{"profileId":"intern-platform"}`, nil), 403)
	expect(test, call(test, disabled, "POST", "/api/demo/requests/no/status", `{"status":"granted"}`, cookie), 403)
	sessions.fail = true
	expect(test, call(test, handler, "GET", "/api/plan", "", cookie), 503)
	expect(test, call(test, handler, "GET", "/healthz", "", nil), 503)
	expect(test, call(test, handler, "POST", "/api/session", `{"profileId":"intern-platform"}`, nil), 503)
	sessions.fail = false
	rotated := call(test, handler, "POST", "/api/session", `{"profileId":"intern-analytics"}`, cookie)
	expect(test, rotated, 200)
	expect(test, call(test, handler, "GET", "/api/session", "", cookie), 401)
	dir := test.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>Onbo</title>"), 0600); err != nil {
		test.Fatal(err)
	}
	assets := filepath.Join(dir, "assets")
	_ = os.Mkdir(assets, 0700)
	_ = os.WriteFile(filepath.Join(assets, "app.js"), []byte("app"), 0600)
	static := NewServer(service, sessions, testAgent{}, Config{StaticDir: dir})
	expect(test, call(test, static, "GET", "/route", "", nil), 200)
	recorder := call(test, static, "GET", "/assets/app.js", "", nil)
	expect(test, recorder, 200)
	if recorder.Body.String() != "app" {
		test.Fatal(recorder.Body.String())
	}
}

func TestStoreInvalidAndRemoteFailure(test *testing.T) {
	service, _, _, _ := setup(test)
	ctx := context.Background()
	user, _ := FindProfile("intern-platform")
	if _, err := service.GetOnboardingPlan(ctx, core.User{ID: "invalid"}); !errors.Is(err, ErrForbidden) {
		test.Fatal(err)
	}
	if _, err := service.ListAccessResources(ctx, core.User{}); !errors.Is(err, ErrForbidden) {
		test.Fatal(err)
	}
	if _, err := service.ListAccessRequests(ctx, core.User{}); !errors.Is(err, ErrForbidden) {
		test.Fatal(err)
	}
	if _, err := service.CreateAccessDraft(ctx, user, "training-repo", strings.Repeat("я", 501)); !errors.Is(err, ErrInvalid) {
		test.Fatal(err)
	}
	if _, err := service.ConfirmAndSubmitAccessDraft(ctx, core.User{}, "missing", true); !errors.Is(err, ErrForbidden) {
		test.Fatal(err)
	}
	if _, err := service.RefreshAccessRequestStatus(ctx, user, "missing"); !errors.Is(err, ErrNotFound) {
		test.Fatal(err)
	}
	if _, err := service.SimulateAccessRequestStatus(ctx, user, "missing", "granted"); !errors.Is(err, ErrNotFound) {
		test.Fatal(err)
	}
	for _, body := range []string{`broken`, `{"id":"x","status":"invented"}`, `{"id":"","status":"pending"}`} {
		remote := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			_, _ = fmt.Fprint(responseWriter, body)
		}))
		service.MockURL = remote.URL
		if _, _, err := service.callAccessService(ctx, "GET", "/", nil); !errors.Is(err, ErrUnavailable) {
			test.Fatal(err)
		}
		remote.Close()
	}
	service.MockURL = "http://127.0.0.1:1"
	if _, _, err := service.callAccessService(ctx, "GET", "/", nil); !errors.Is(err, ErrUnavailable) {
		test.Fatal(err)
	}
	if _, err := NewStore(ctx, "postgres://invalid:invalid@127.0.0.1:1/unavailable?connect_timeout=1", "", "x"); err == nil {
		test.Fatal("expected database error")
	}
}

func TestRedisSessionsAndLimit(test *testing.T) {
	address := os.Getenv("ONBO_TEST_REDIS_ADDR")
	if address == "" {
		test.Skip("ONBO_TEST_REDIS_ADDR required")
	}
	sessions := NewRedisSessions(address, "")
	defer sessions.Close()
	ctx := context.Background()
	if err := sessions.Ping(ctx); err != nil {
		test.Fatal(err)
	}
	identifier := "test-" + generateIdentifier()
	token, err := sessions.CreateSession(ctx, identifier)
	if err != nil {
		test.Fatal(err)
	}
	got, err := sessions.GetSessionUserID(ctx, token)
	if err != nil || got != identifier {
		test.Fatal(got, err)
	}
	if _, err = sessions.GetSessionUserID(ctx, "bad"); !errors.Is(err, ErrForbidden) {
		test.Fatal(err)
	}
	for itemIndex := 0; itemIndex < 21; itemIndex++ {
		allow, err := sessions.ConsumeChatQuota(ctx, identifier)
		if err != nil || allow != (itemIndex < 20) {
			test.Fatal(itemIndex, allow, err)
		}
	}
	if err = sessions.DeleteSession(ctx, token); err != nil {
		test.Fatal(err)
	}
	if _, err = sessions.GetSessionUserID(ctx, token); !errors.Is(err, ErrForbidden) {
		test.Fatal(err)
	}
	_ = sessions.Client.Del(ctx, "onbo:limit:"+identifier).Err()
}
