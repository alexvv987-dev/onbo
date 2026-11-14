package mock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"onbo/internal/config"
)

func dbForTest(test *testing.T) string {
	test.Helper()
	raw := os.Getenv("ONBO_TEST_DATABASE_URL")
	if raw == "" {
		test.Skip("ONBO_TEST_DATABASE_URL required")
	}
	databasePool, err := pgxpool.New(context.Background(), raw)
	if err != nil {
		test.Fatal(err)
	}
	identifierBytes := make([]byte, 12)
	_, _ = rand.Read(identifierBytes)
	schema := "mock_test_" + hex.EncodeToString(identifierBytes)
	if _, err = databasePool.Exec(context.Background(), "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
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

func TestConfiguredRequestBodyLimit(test *testing.T) {
	settings := config.DefaultMock()
	settings.MaxRequestBytes = 32
	service, err := New(context.Background(), dbForTest(test), "secret", settings)
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(service.Close)
	request(test, service, "POST", "/requests", `{"key":"1234567890123456","userId":"intern-platform","resourceId":"training-repo"}`, "secret", 400)
}
func request(test *testing.T, handler http.Handler, method, path, body, token string, status int) *httptest.ResponseRecorder {
	test.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("X-Mock-Token", token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != status {
		test.Fatalf("%s %s got=%d want=%d body=%s", method, path, recorder.Code, status, recorder.Body.String())
	}
	return recorder
}
func TestDurableIdempotentLifecycle(test *testing.T) {
	ctx := context.Background()
	db := dbForTest(test)
	service, err := New(ctx, db, "secret")
	if err != nil {
		test.Fatal(err)
	}
	defer service.Close()
	request(test, service, "GET", "/healthz", "", "", 200)
	request(test, service, "GET", "/unknown", "", "", 401)
	request(test, service, "GET", "/unknown", "", "secret", 404)
	body := `{"key":"test-operation-123456","userId":"intern-platform","resourceId":"training-repo"}`
	responseWriter := request(test, service, "POST", "/requests", body, "secret", 200)
	var first Result
	_ = json.Unmarshal(responseWriter.Body.Bytes(), &first)
	if first.Status != "pending" || first.ID == "" {
		test.Fatal(first)
	}
	responseWriter = request(test, service, "POST", "/requests", body, "secret", 200)
	var again Result
	_ = json.Unmarshal(responseWriter.Body.Bytes(), &again)
	if first.ID != again.ID {
		test.Fatal("duplicate")
	}
	request(test, service, "GET", "/requests/by-key/test-operation-123456", "", "secret", 200)
	request(test, service, "GET", "/requests/by-key/missing", "", "secret", 404)
	for _, bad := range []string{`{`, `{} {}`, `{"unknown":true}`, `{"key":"short","userId":"intern-platform","resourceId":"training-repo"}`, `{"key":"test-operation-123456","userId":"intern-analytics","resourceId":"training-repo"}`, `{"key":"test-operation-123456","userId":"intern-platform","resourceId":"secret"}`, strings.Repeat(" ", 17000)} {
		request(test, service, "POST", "/requests", bad, "secret", 400)
	}
	request(test, service, "POST", "/requests/"+first.ID+"/status", `{"status":"pending"}`, "secret", 400)
	request(test, service, "POST", "/requests/missing/status", `{"status":"granted"}`, "secret", 409)
	request(test, service, "POST", "/requests/"+first.ID+"/status", `{"status":"granted"}`, "secret", 200)
	request(test, service, "POST", "/requests/"+first.ID+"/status", `{"status":"granted"}`, "secret", 200)
	request(test, service, "POST", "/requests/"+first.ID+"/status", `{"status":"rejected"}`, "secret", 409)
	secondBody := strings.ReplaceAll(body, "test-operation-123456", "another-operation-123456")
	responseWriter = request(test, service, "POST", "/requests", secondBody, "secret", 200)
	var second Result
	_ = json.Unmarshal(responseWriter.Body.Bytes(), &second)
	request(test, service, "POST", "/requests/"+second.ID+"/status", `{"status":"rejected"}`, "secret", 200)
	// A service restart retains external IDs and terminal outcomes.
	reopened, err := New(ctx, db, "secret")
	if err != nil {
		test.Fatal(err)
	}
	defer reopened.Close()
	responseWriter = request(test, reopened, "GET", "/requests/by-key/test-operation-123456", "", "secret", 200)
	var persisted Result
	_ = json.Unmarshal(responseWriter.Body.Bytes(), &persisted)
	if persisted.Status != "granted" || persisted.ID != first.ID {
		test.Fatal(persisted)
	}
	// The same durable key cannot be rebound to a different owner/resource.
	_, err = service.Pool.Exec(ctx, "UPDATE onbo_mock_requests SET user_id='different' WHERE key='test-operation-123456'")
	if err != nil {
		test.Fatal(err)
	}
	request(test, service, "POST", "/requests", body, "secret", 409)
}
func TestUnavailableDatabaseAndConfiguration(test *testing.T) {
	ctx := context.Background()
	if _, err := New(ctx, "anything", ""); err == nil {
		test.Fatal("missing token accepted")
	}
	if _, err := New(ctx, "postgres://%zz", "token"); err == nil {
		test.Fatal("invalid URL accepted")
	}
	if _, err := New(ctx, "postgres://none:none@127.0.0.1:1/none?connect_timeout=1", "token"); err == nil {
		test.Fatal("unavailable database accepted")
	}
	service, err := New(ctx, dbForTest(test), "token")
	if err != nil {
		test.Fatal(err)
	}
	service.Close()
	request(test, service, "GET", "/healthz", "", "", 503)
	request(test, service, "POST", "/requests", `{"key":"test-operation-123456","userId":"intern-platform","resourceId":"training-repo"}`, "token", 500)
	request(test, service, "GET", "/requests/by-key/test-operation-123456", "", "token", 500)
	request(test, service, "POST", "/requests/REQ-x/status", `{"status":"granted"}`, "token", 500)
}
