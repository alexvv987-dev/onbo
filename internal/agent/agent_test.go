package agent

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"onbo/internal/core"
)

type fakeTools struct {
	err       error
	sources   []core.Source
	users     []core.User
	resources []core.Resource
	requests  []core.AccessRequest
	drafts    int
}

func (tools *fakeTools) GetOnboardingPlan(_ context.Context, user core.User) (core.Plan, error) {
	tools.users = append(tools.users, user)
	return core.Plan{NextStep: "Прочитать инструкцию"}, tools.err
}
func (tools *fakeTools) SearchKnowledge(_ context.Context, user core.User, _ string) ([]core.Source, error) {
	tools.users = append(tools.users, user)
	return tools.sources, tools.err
}
func (tools *fakeTools) ListAccessResources(_ context.Context, user core.User) ([]core.Resource, error) {
	tools.users = append(tools.users, user)
	return tools.resources, tools.err
}
func (tools *fakeTools) CreateAccessDraft(_ context.Context, user core.User, id, reason string) (core.Draft, error) {
	tools.users = append(tools.users, user)
	tools.drafts++
	return core.Draft{ID: "draft-1", ResourceID: id, Reason: reason}, tools.err
}
func (tools *fakeTools) ListAccessRequests(_ context.Context, user core.User) ([]core.AccessRequest, error) {
	tools.users = append(tools.users, user)
	return tools.requests, tools.err
}

var testUser = core.User{ID: "current-user", Team: "platform"}
var source = core.Source{ID: "intro", Title: "Первый день", Excerpt: "Сначала прочитайте инструкцию.", Label: "Тестовые данные"}

func TestExecute(test *testing.T) {
	tools := &fakeTools{sources: []core.Source{source}, resources: []core.Resource{{ID: "training-repo"}}, requests: []core.AccessRequest{{ID: "mine", Status: "pending"}}}
	for _, testCase := range []struct{ name, args string }{
		{"get_onboarding_plan", `{}`}, {"search_knowledge", `{"query":"доступ"}`}, {"list_access_resources", `{}`},
		{"draft_access_request", `{"resourceId":"training-repo","reason":"Первая задача"}`},
		{"get_access_request_status", `{"requestId":"mine"}`}, {"get_access_request_status", `{"requestId":""}`},
		{"get_access_request_status", `{"requestId":"someone-else"}`},
	} {
		test.Run(testCase.name+testCase.args, func(test *testing.T) {
			state := toolResults{}
			result, err := executeTool(context.Background(), tools, testUser, &functionCall{Name: testCase.name, Arguments: json.RawMessage(testCase.args)}, &state)
			if err != nil || !json.Valid([]byte(result)) || !state.used {
				test.Fatalf("result=%s error=%v", result, err)
			}
			if strings.Contains(testCase.args, "someone-else") && !strings.Contains(result, "недоступна") {
				test.Fatal("foreign request disclosed")
			}
		})
	}
	for _, user := range tools.users {
		if user != testUser {
			test.Fatal("identity changed")
		}
	}
	if tools.drafts != 1 {
		test.Fatal("draft not prepared")
	}
	state := toolResults{}
	for range 2 {
		_, _ = executeTool(context.Background(), tools, testUser, &functionCall{Name: "search_knowledge", Arguments: json.RawMessage(`{"query":"доступ"}`)}, &state)
	}
	if len(state.sources) != 1 {
		test.Fatal("sources not deduplicated")
	}
}

func TestArgumentsRejectedBeforeExecution(test *testing.T) {
	for _, testCase := range []struct{ name, args string }{
		{"get_onboarding_plan", `{"userId":"other"}`}, {"get_onboarding_plan", `null`}, {"get_onboarding_plan", `[]`},
		{"search_knowledge", `{"query":""}`}, {"search_knowledge", `{"query":"x","userId":"other"}`},
		{"search_knowledge", `{"query":"` + strings.Repeat("x", 501) + `"}`},
		{"list_access_resources", `{"all":true}`}, {"draft_access_request", `{"resourceId":"repo"}`},
		{"draft_access_request", `{"resourceId":"repo","reason":"x","confirmed":true}`},
		{"get_access_request_status", `{}`}, {"get_access_request_status", `{"requestId":null}`},
		{"get_access_request_status", `{"requestId":"` + strings.Repeat("x", 201) + `"}`},
		{"submit_access_request", `{}`}, {"unknown", `{}`}, {"search_knowledge", `  `}, {"get_onboarding_plan", `{} {}`},
		{"get_onboarding_plan", `{`}, {"get_onboarding_plan", strings.Repeat(" ", 5000)},
	} {
		test.Run(testCase.name+testCase.args[:min(len(testCase.args), 30)], func(test *testing.T) {
			tools := &fakeTools{}
			_, err := executeTool(context.Background(), tools, testUser, &functionCall{Name: testCase.name, Arguments: json.RawMessage(testCase.args)}, &toolResults{})
			if !errors.Is(err, ErrArguments) || len(tools.users) != 0 {
				test.Fatalf("error=%v calls=%d", err, len(tools.users))
			}
		})
	}
	if _, err := executeTool(context.Background(), &fakeTools{}, testUser, nil, &toolResults{}); err != ErrArguments {
		test.Fatal(err)
	}
}

func TestToolErrorsAndSourceLimit(test *testing.T) {
	tools := &fakeTools{err: errors.New("database password PRIVATE")}
	result, err := executeTool(context.Background(), tools, testUser, &functionCall{Name: "search_knowledge", Arguments: json.RawMessage(`{"query":"x"}`)}, &toolResults{})
	if err != nil || strings.Contains(result, "PRIVATE") || !strings.Contains(result, "error") {
		test.Fatalf("%s %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = executeTool(ctx, tools, testUser, &functionCall{Name: "get_onboarding_plan", Arguments: json.RawMessage(`{}`)}, &toolResults{})
	if !errors.Is(err, context.Canceled) {
		test.Fatal(err)
	}
	tools.err = nil
	tools.sources = make([]core.Source, 10)
	for itemIndex := range tools.sources {
		tools.sources[itemIndex].ID = fmt.Sprint(itemIndex)
	}
	state := toolResults{}
	_, err = executeTool(context.Background(), tools, testUser, &functionCall{Name: "search_knowledge", Arguments: json.RawMessage(`{"query":"x"}`)}, &state)
	if err != nil || len(state.sources) != 5 {
		test.Fatalf("sources=%d error=%v", len(state.sources), err)
	}
}

func TestHistoryBoundaries(test *testing.T) {
	input := make([]core.Message, 20)
	for itemIndex := range input {
		input[itemIndex] = core.Message{Role: "user", Text: strings.Repeat("я", 5000)}
	}
	input[19].Role = "system"
	got := buildHistoryMessages(input)
	if len(got) != 11 || len([]rune(got[0].Content)) != 1000 {
		test.Fatalf("history length %d", len(got))
	}
	if len(buildHistoryMessages([]core.Message{{Role: "assistant", Text: "ok"}})) != 1 {
		test.Fatal("assistant dropped")
	}
}

func newTestClient(test *testing.T, tools *fakeTools, handler http.HandlerFunc) *Client {
	test.Helper()
	server := httptest.NewServer(handler)
	test.Cleanup(server.Close)
	client, err := New(Config{AuthorizationKey: "test-only-key", Model: "test-model", APIURL: server.URL, OAuthURL: server.URL + "/oauth"}, tools)
	if err != nil {
		test.Fatal(err)
	}
	return client
}

func oauthReply(responseWriter http.ResponseWriter) {
	_ = json.NewEncoder(responseWriter).Encode(map[string]any{"access_token": "test-bearer", "expires_at": time.Now().Add(30 * time.Minute).UnixMilli()})
}
func reply(responseWriter http.ResponseWriter, message message, finish string) {
	_ = json.NewEncoder(responseWriter).Encode(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}})
}

func TestNativeToolLoopAndState(test *testing.T) {
	tools := &fakeTools{sources: []core.Source{source}}
	var auth, calls int
	client := newTestClient(test, tools, func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/oauth" {
			auth++
			if request.Header.Get("Authorization") != "Basic test-only-key" || len(request.Header.Get("RqUID")) != 36 {
				test.Error("bad OAuth headers")
			}
			_ = request.ParseForm()
			if request.Form.Get("scope") != "GIGACHAT_API_PERS" {
				test.Error("bad scope")
			}
			oauthReply(responseWriter)
			return
		}
		if request.Header.Get("Authorization") != "Bearer test-bearer" {
			test.Error("bad bearer")
		}
		var body struct {
			Model        string           `json:"model"`
			Messages     []message        `json:"messages"`
			Functions    []toolDefinition `json:"functions"`
			FunctionCall string           `json:"function_call"`
			Stream       bool             `json:"stream"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			test.Error(err)
		}
		if body.Model != "test-model" || len(body.Functions) != 5 || body.FunctionCall != "auto" || body.Stream {
			test.Error("bad native request")
		}
		calls++
		if calls == 1 {
			reply(responseWriter, message{Role: "assistant", FunctionsStateID: "native-state", FunctionCall: &functionCall{Name: "search_knowledge", Arguments: json.RawMessage(`{"query":"первый день"}`)}}, "function_call")
			return
		}
		last := body.Messages[len(body.Messages)-1]
		previous := body.Messages[len(body.Messages)-2]
		if last.Role != "function" || last.Name != "search_knowledge" || !json.Valid([]byte(last.Content)) || previous.FunctionsStateID != "native-state" {
			test.Errorf("function state lost: %+v", body.Messages)
		}
		reply(responseWriter, message{Role: "assistant", Content: "Сначала прочитайте инструкцию."}, "stop")
	})
	got, err := client.GenerateReply(context.Background(), testUser, []core.Message{{Role: "assistant", Text: "Привет"}}, "Что делать в первый день?")
	if err != nil || len(got.Sources) != 1 || !strings.Contains(got.Message, "прочитайте") {
		test.Fatalf("%+v %v", got, err)
	}
	if auth != 1 || calls != 2 {
		test.Fatalf("auth=%d model=%d", auth, calls)
	}
}

func TestDirectAnswerRegeneratedWithEvidence(test *testing.T) {
	var calls int
	client := newTestClient(test, &fakeTools{sources: []core.Source{source}}, func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/oauth" {
			oauthReply(responseWriter)
			return
		}
		calls++
		if calls == 1 {
			reply(responseWriter, message{Content: "Придуманный ответ"}, "stop")
		} else {
			reply(responseWriter, message{Content: "Ответ по найденной инструкции"}, "stop")
		}
	})
	got, err := client.GenerateReply(context.Background(), testUser, nil, "Первый день?")
	if err != nil || calls != 3 || strings.Contains(got.Message, "Придуманный") {
		test.Fatalf("%+v %v", got, err)
	}
}

func TestPrematureAnswerRequestsNativeToolAgain(test *testing.T) {
	var calls int
	var trace []ToolTrace
	client := newTestClient(test, &fakeTools{sources: []core.Source{source}}, func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/oauth" {
			oauthReply(responseWriter)
			return
		}
		calls++
		var body struct {
			Messages []message `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			test.Error(err)
		}
		switch calls {
		case 1:
			reply(responseWriter, message{Content: "Спросите кого-нибудь в команде"}, "stop")
		case 2:
			if !strings.Contains(body.Messages[0].Content, "ПРОВЕРКА ПОРЯДКА") {
				test.Error("missing grounding correction")
			}
			reply(responseWriter, message{FunctionCall: &functionCall{Name: "search_knowledge", Arguments: json.RawMessage(`{"query":"наставник"}`)}}, "function_call")
		default:
			reply(responseWriter, message{Content: "Ответ по разрешённой инструкции"}, "stop")
		}
	})
	ctx := ObserveTools(context.Background(), func(event ToolTrace) { trace = append(trace, event) })
	got, err := client.GenerateReply(ctx, testUser, nil, "К кому обратиться за помощью?")
	if err != nil || calls != 3 || len(got.Sources) != 1 || len(trace) != 1 || !trace[0].ModelSelected || !trace[0].Executed {
		test.Fatalf("reply=%+v trace=%+v err=%v", got, trace, err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fixture roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fixture(request)
}

func TestTransientRequestsRetryWithoutReplayingTools(test *testing.T) {
	for _, status := range []int{429, 502, 503, 504} {
		test.Run(fmt.Sprint(status), func(test *testing.T) {
			var calls int
			client := newTestClient(test, &fakeTools{}, func(responseWriter http.ResponseWriter, request *http.Request) {
				calls++
				if calls == 1 {
					responseWriter.WriteHeader(status)
					return
				}
				oauthReply(responseWriter)
			})
			if _, err := client.getAccessToken(context.Background()); err != nil || calls != 2 {
				test.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
	for _, transient := range []bool{true, false} {
		test.Run(fmt.Sprint(transient), func(test *testing.T) {
			var calls int
			transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if transient {
						return nil, io.ErrUnexpectedEOF
					}
					return nil, x509.UnknownAuthorityError{}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"access_token":"retry-test","expires_at":%d}`, time.Now().Add(time.Hour).UnixMilli()))), Request: request}, nil
			})
			client, err := New(Config{AuthorizationKey: "test-only", Model: "test-model", HTTPClient: &http.Client{Transport: transport}}, &fakeTools{})
			if err != nil {
				test.Fatal(err)
			}
			_, err = client.getAccessToken(context.Background())
			if transient {
				if err != nil || calls != 2 {
					test.Fatalf("calls=%d err=%v", calls, err)
				}
			} else if err == nil || calls != 1 {
				test.Fatalf("certificate failure retried calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestNoSourceAndDraftGrounding(test *testing.T) {
	for _, draft := range []bool{false, true} {
		test.Run(fmt.Sprint(draft), func(test *testing.T) {
			var calls int
			client := newTestClient(test, &fakeTools{}, func(responseWriter http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/oauth" {
					oauthReply(responseWriter)
					return
				}
				calls++
				if draft && calls == 1 {
					reply(responseWriter, message{FunctionCall: &functionCall{Name: "draft_access_request", Arguments: json.RawMessage(`{"resourceId":"training-repo","reason":"первая задача"}`)}}, "function_call")
				} else {
					reply(responseWriter, message{Content: "Доступ уже выдан без подтверждения!"}, "stop")
				}
			})
			got, err := client.GenerateReply(context.Background(), testUser, nil, "Нужен доступ")
			if err != nil {
				test.Fatal(err)
			}
			if draft {
				if got.Draft == nil || !strings.Contains(got.Message, "ещё не отправлена") {
					test.Fatalf("%+v", got)
				}
			} else {
				if len(got.Sources) != 0 || !strings.Contains(got.Message, "не найдено") {
					test.Fatalf("%+v", got)
				}
			}
		})
	}
}

func TestReplyFailureModes(test *testing.T) {
	for _, testCase := range []struct {
		name, body string
		status     int
		want       error
	}{
		{"provider-error", `key-is-private`, 429, ErrUnavailable}, {"bad-json", `oops`, 200, ErrUnavailable},
		{"empty-choices", `{"choices":[]}`, 200, ErrUnavailable},
		{"empty-content", `{"choices":[{"message":{"content":""}}]}`, 200, ErrUnavailable},
		{"truncated", `{"choices":[{"message":{"content":"half"},"finish_reason":"length"}]}`, 200, ErrUnavailable},
		{"unknown-tool", `{"choices":[{"message":{"function_call":{"name":"send_without_confirmation","arguments":{}}},"finish_reason":"function_call"}]}`, 200, ErrArguments},
		{"tool-loop", `{"choices":[{"message":{"function_call":{"name":"get_onboarding_plan","arguments":{}}},"finish_reason":"function_call"}]}`, 200, ErrLimit},
		{"oversized", strings.Repeat("x", (1<<20)+1), 200, ErrUnavailable},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			client := newTestClient(test, &fakeTools{}, func(responseWriter http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/oauth" {
					oauthReply(responseWriter)
					return
				}
				responseWriter.WriteHeader(testCase.status)
				_, _ = responseWriter.Write([]byte(testCase.body))
			})
			_, err := client.GenerateReply(context.Background(), testUser, nil, "вопрос")
			if !errors.Is(err, testCase.want) || strings.Contains(err.Error(), "private") {
				test.Fatalf("%v", err)
			}
		})
	}
	client := newTestClient(test, &fakeTools{}, func(responseWriter http.ResponseWriter, request *http.Request) { oauthReply(responseWriter) })
	if _, err := client.GenerateReply(context.Background(), testUser, nil, ""); err == nil {
		test.Fatal("empty message accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.GenerateReply(ctx, testUser, nil, "вопрос"); !errors.Is(err, context.Canceled) {
		test.Fatal(err)
	}
}

func TestOAuthCacheAndRenewal(test *testing.T) {
	var calls atomic.Int32
	client := newTestClient(test, &fakeTools{}, func(responseWriter http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		oauthReply(responseWriter)
	})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.getAccessToken(context.Background()); err != nil {
				test.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		test.Fatalf("duplicate refresh: %d", calls.Load())
	}
	client.expires = time.Now().Add(20 * time.Second)
	if _, err := client.getAccessToken(context.Background()); err != nil || calls.Load() != 2 {
		test.Fatalf("%v count=%d", err, calls.Load())
	}
	for _, body := range []string{`{}`, `{"access_token":"x","expires_at":1}`, `invalid`} {
		responseWriter := newTestClient(test, &fakeTools{}, func(responseWriter http.ResponseWriter, request *http.Request) {
			_, _ = responseWriter.Write([]byte(body))
		})
		if _, err := responseWriter.getAccessToken(context.Background()); !errors.Is(err, ErrUnavailable) {
			test.Fatal(err)
		}
	}
}

func TestConfigAndTLS(test *testing.T) {
	for _, config := range []Config{{}, {AuthorizationKey: "x"}, {AuthorizationKey: "x", Model: "x", APIURL: "http://example.com"}, {AuthorizationKey: "x", Model: "x", OAuthURL: ":bad"}, {AuthorizationKey: "x", Model: "x", APIURL: "https://u:secret@example.com"}, {AuthorizationKey: "x", Model: "x", APIURL: "https://example.com?key=x"}} {
		if _, err := New(config, &fakeTools{}); err == nil {
			test.Fatalf("accepted %+v", config)
		}
	}
	if _, err := New(Config{AuthorizationKey: "x", Model: "x"}, nil); err == nil {
		test.Fatal("nil tools")
	}
	for _, mode := range []string{"demo", "unknown", "gigachat", ""} {
		test.Run(mode, func(test *testing.T) {
			test.Setenv("LLM_MODE", mode)
			test.Setenv("GIGACHAT_AUTHORIZATION_KEY", "")
			test.Setenv("GIGACHAT_MODEL", "")
			got, err := NewFromEnv(&fakeTools{})
			if mode == "demo" {
				if _, ok := got.(*Demo); !ok || err != nil {
					test.Fatal(err)
				}
			} else if err == nil {
				test.Fatal("missing config accepted")
			}
		})
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) { oauthReply(responseWriter) }))
	defer server.Close()
	path := filepath.Join(test.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		test.Fatal(err)
	}
	client, err := New(Config{AuthorizationKey: "x", Model: "x", APIURL: server.URL, OAuthURL: server.URL, CAFile: path}, &fakeTools{})
	if err != nil {
		test.Fatal(err)
	}
	if _, err = client.getAccessToken(context.Background()); err != nil {
		test.Fatal(err)
	}
	transport := client.http.Transport.(*http.Transport)
	if transport.TLSClientConfig.InsecureSkipVerify {
		test.Fatal("TLS verification disabled")
	}
	if _, err := New(Config{AuthorizationKey: "x", Model: "x", CAFile: path + "missing"}, &fakeTools{}); err == nil {
		test.Fatal("missing CA accepted")
	}
	if err = os.WriteFile(path, []byte("not a certificate"), 0600); err != nil {
		test.Fatal(err)
	}
	if _, err := New(Config{AuthorizationKey: "x", Model: "x", CAFile: path}, &fakeTools{}); err == nil {
		test.Fatal("bad CA accepted")
	}
}

func TestRedirectDoesNotLeakCredentials(test *testing.T) {
	var reached bool
	target := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) { reached = true }))
	defer target.Close()
	client := newTestClient(test, &fakeTools{}, func(responseWriter http.ResponseWriter, request *http.Request) {
		http.Redirect(responseWriter, request, target.URL, http.StatusTemporaryRedirect)
	})
	if _, err := client.getAccessToken(context.Background()); err == nil || reached {
		test.Fatal("redirect followed")
	}
}

func TestDemoPaths(test *testing.T) {
	tools := &fakeTools{sources: []core.Source{source}, resources: []core.Resource{{ID: "training-repo"}}}
	demo := NewDemo(tools)
	for _, testCase := range []struct {
		text, want string
		draft      bool
	}{
		{"Покажи план", "Следующий шаг", false}, {"Как начать первую задачу?", "Первый день", false},
		{"Подготовь заявку на доступ", "ещё не отправлена", true}, {"Какой статус моей заявки?", "нет отправленных", false},
	} {
		got, err := demo.GenerateReply(context.Background(), testUser, nil, testCase.text)
		if err != nil || !strings.Contains(got.Message, testCase.want) || (got.Draft != nil) != testCase.draft {
			test.Fatalf("%+v %v", got, err)
		}
	}
	tools.requests = []core.AccessRequest{{ID: "REQ-1", Status: "pending"}}
	got, err := demo.GenerateReply(context.Background(), testUser, nil, "Статус заявки")
	if err != nil || !strings.Contains(got.Message, "REQ-1") {
		test.Fatal(got, err)
	}
	tools.resources = nil
	got, err = demo.GenerateReply(context.Background(), testUser, nil, "Нужен доступ")
	if err != nil || got.Draft != nil || !strings.Contains(got.Message, "нет доступных") {
		test.Fatal(got, err)
	}
	tools.sources = nil
	got, err = demo.GenerateReply(context.Background(), testUser, nil, "Сколько стоит обед?")
	if err != nil || !strings.Contains(got.Message, "не найдено") {
		test.Fatal(got, err)
	}
	tools.sources = make([]core.Source, 8)
	got, err = demo.GenerateReply(context.Background(), testUser, nil, "Инструкция")
	if err != nil || len(got.Sources) != 5 {
		test.Fatal(got, err)
	}
	for _, query := range []string{"План", "Статус", "Инструкция", "Нужен доступ"} {
		tools.err = errors.New("unavailable")
		if _, err := demo.GenerateReply(context.Background(), testUser, nil, query); err == nil {
			test.Fatal("tool error hidden")
		}
	}
	if _, err := demo.GenerateReply(context.Background(), testUser, nil, ""); err == nil {
		test.Fatal("empty accepted")
	}
	if _, err := NewDemo(nil).GenerateReply(context.Background(), testUser, nil, "x"); err == nil {
		test.Fatal("nil tools")
	}
	if !reflect.DeepEqual(toolDefinitions()[0].Parameters["required"], []string{}) {
		test.Fatal("schema required must be array")
	}
}
