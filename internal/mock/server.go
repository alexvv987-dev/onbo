package mock

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"onbo/internal/config"
	"onbo/internal/core"
)

const (
	accessGrantedMessage            = "Доступ выдан тестовым сервисом. Можно приступить к первой задаче."
	accessRejectedMessage           = "Тестовый сервис отклонил заявку. Обратитесь к наставнику в #platform-interns."
	databaseUnavailableMessage      = "database unavailable"
	idempotencyConflictMessage      = "key belongs to another request"
	initializeMockSchemaSQL         = "CREATE TABLE IF NOT EXISTS onbo_mock_requests(id text PRIMARY KEY,key text UNIQUE NOT NULL,user_id text NOT NULL,resource_id text NOT NULL,status text NOT NULL DEFAULT '" + core.RequestPending + "',message text NOT NULL DEFAULT 'Заявка принята. Ожидает согласования в тестовом сервисе.')"
	insertMockRequestQuery          = "INSERT INTO onbo_mock_requests(id,key,user_id,resource_id) VALUES($1,$2,$3,$4) ON CONFLICT(key) DO NOTHING"
	invalidJSONMessage              = "invalid JSON"
	invalidRequestMessage           = "invalid request"
	invalidStatusMessage            = "invalid status"
	maxIdempotencyKeyLength         = 80
	minIdempotencyKeyLength         = 16
	missingMockTokenMessage         = "MOCK_TOKEN must be configured"
	notFoundMessage                 = "not found"
	requestIDPrefix                 = "REQ-"
	routeGETRequestsByKeyKey        = "GET /requests/by-key/{key}"
	routeHealthz                    = "/healthz"
	routePOSTRequests               = "POST /requests"
	routePOSTRequestsIDStatus       = "POST /requests/{id}/status"
	selectMockRequestByKeyQuery     = "SELECT id,status,message FROM onbo_mock_requests WHERE key=$1"
	selectMockRequestWithOwnerQuery = "SELECT id,status,message,user_id,resource_id FROM onbo_mock_requests WHERE key=$1"
	terminalConflictMessage         = "request missing or terminal"
	unauthorizedMessage             = "unauthorized"
	updateMockRequestStatusQuery    = "UPDATE onbo_mock_requests SET status=$1,message=$2 WHERE id=$3 AND (status='" + core.RequestPending + "' OR status=$1) RETURNING id,status,message"
)

type Result struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
type Server struct {
	Pool     *pgxpool.Pool
	token    string
	mux      *http.ServeMux
	settings Config
}

func New(ctx context.Context, databaseURL, token string, options ...Config) (*Server, error) {
	settings := config.DefaultMock()
	if len(options) > 0 {
		settings = options[0]
	}
	if token == "" {
		return nil, errors.New(missingMockTokenMessage)
	}
	databasePool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err = databasePool.Ping(ctx); err != nil {
		databasePool.Close()
		return nil, err
	}
	_, err = databasePool.Exec(ctx, initializeMockSchemaSQL)
	if err != nil {
		databasePool.Close()
		return nil, err
	}
	server := &Server{Pool: databasePool, token: token, mux: http.NewServeMux(), settings: settings}
	server.mux.HandleFunc(routePOSTRequests, server.createAccessRequestHandler)
	server.mux.HandleFunc(routeGETRequestsByKeyKey, server.getAccessRequestByKeyHandler)
	server.mux.HandleFunc(routePOSTRequestsIDStatus, server.updateAccessRequestStatusHandler)
	server.mux.HandleFunc("/", func(responseWriter http.ResponseWriter, request *http.Request) {
		send(responseWriter, http.StatusNotFound, map[string]string{"error": notFoundMessage})
	})
	return server, nil
}
func (server *Server) Close() { server.Pool.Close() }
func (server *Server) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	responseWriter.Header().Set(core.HeaderContentType, core.ContentTypeJSONUTF8)
	if request.URL.Path == routeHealthz && request.Method == http.MethodGet {
		ctx, cancel := context.WithTimeout(request.Context(), server.settings.HealthTimeout)
		defer cancel()
		if server.Pool.Ping(ctx) != nil {
			send(responseWriter, http.StatusServiceUnavailable, map[string]string{"error": databaseUnavailableMessage})
			return
		}
		send(responseWriter, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(request.Header.Get(core.HeaderMockToken)), []byte(server.token)) != 1 {
		send(responseWriter, http.StatusUnauthorized, map[string]string{"error": unauthorizedMessage})
		return
	}
	server.mux.ServeHTTP(responseWriter, request)
}
func (server *Server) createAccessRequestHandler(responseWriter http.ResponseWriter, request *http.Request) {
	var requestBody struct {
		Key        string `json:"key"`
		UserID     string `json:"userId"`
		ResourceID string `json:"resourceId"`
	}
	if !server.readJSONRequest(responseWriter, request, &requestBody) {
		return
	}
	if len(requestBody.Key) < minIdempotencyKeyLength || len(requestBody.Key) > maxIdempotencyKeyLength || requestBody.UserID != core.PlatformProfileID || requestBody.ResourceID != core.TrainingResourceID {
		send(responseWriter, http.StatusBadRequest, map[string]string{"error": invalidRequestMessage})
		return
	}
	_, err := server.Pool.Exec(request.Context(), insertMockRequestQuery, requestIDPrefix+requestBody.Key, requestBody.Key, requestBody.UserID, requestBody.ResourceID)
	if err != nil {
		send(responseWriter, http.StatusInternalServerError, map[string]string{"error": databaseUnavailableMessage})
		return
	}
	var owner, resource string
	var responseValue Result
	err = server.Pool.QueryRow(request.Context(), selectMockRequestWithOwnerQuery, requestBody.Key).Scan(&responseValue.ID, &responseValue.Status, &responseValue.Message, &owner, &resource)
	if err != nil {
		send(responseWriter, http.StatusInternalServerError, map[string]string{"error": databaseUnavailableMessage})
		return
	}
	if owner != requestBody.UserID || resource != requestBody.ResourceID {
		send(responseWriter, http.StatusConflict, map[string]string{"error": idempotencyConflictMessage})
		return
	}
	send(responseWriter, http.StatusOK, responseValue)
}
func (server *Server) getAccessRequestByKeyHandler(responseWriter http.ResponseWriter, request *http.Request) {
	var responseValue Result
	err := server.Pool.QueryRow(request.Context(), selectMockRequestByKeyQuery, request.PathValue("key")).Scan(&responseValue.ID, &responseValue.Status, &responseValue.Message)
	if errors.Is(err, pgx.ErrNoRows) {
		send(responseWriter, http.StatusNotFound, map[string]string{"error": notFoundMessage})
		return
	}
	if err != nil {
		send(responseWriter, http.StatusInternalServerError, map[string]string{"error": databaseUnavailableMessage})
		return
	}
	send(responseWriter, http.StatusOK, responseValue)
}
func (server *Server) updateAccessRequestStatusHandler(responseWriter http.ResponseWriter, request *http.Request) {
	var requestBody struct {
		Status string `json:"status"`
	}
	if !server.readJSONRequest(responseWriter, request, &requestBody) {
		return
	}
	if requestBody.Status != core.RequestGranted && requestBody.Status != core.RequestRejected {
		send(responseWriter, http.StatusBadRequest, map[string]string{"error": invalidStatusMessage})
		return
	}
	message := accessGrantedMessage
	if requestBody.Status == core.RequestRejected {
		message = accessRejectedMessage
	}
	var responseValue Result
	err := server.Pool.QueryRow(request.Context(), updateMockRequestStatusQuery, requestBody.Status, message, request.PathValue("id")).Scan(&responseValue.ID, &responseValue.Status, &responseValue.Message)
	if errors.Is(err, pgx.ErrNoRows) {
		send(responseWriter, http.StatusConflict, map[string]string{"error": terminalConflictMessage})
		return
	}
	if err != nil {
		send(responseWriter, http.StatusInternalServerError, map[string]string{"error": databaseUnavailableMessage})
		return
	}
	send(responseWriter, http.StatusOK, responseValue)
}
func (server *Server) readJSONRequest(responseWriter http.ResponseWriter, request *http.Request, responseValue any) bool {
	request.Body = http.MaxBytesReader(responseWriter, request.Body, server.settings.MaxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(responseValue) != nil {
		send(responseWriter, http.StatusBadRequest, map[string]string{"error": invalidJSONMessage})
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		send(responseWriter, http.StatusBadRequest, map[string]string{"error": invalidJSONMessage})
		return false
	}
	return true
}
func send(responseWriter http.ResponseWriter, code int, responseValue any) {
	responseWriter.WriteHeader(code)
	_ = json.NewEncoder(responseWriter).Encode(responseValue)
}
