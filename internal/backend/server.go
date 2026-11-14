package backend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"onbo/internal/config"
	"onbo/internal/core"
)

const (
	cacheNoStore                     = "no-store"
	chatRateLimitMessage             = "Слишком много сообщений. Попробуйте через минуту"
	contentSecurityPolicy            = "default-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
	contentTypeNoSniff               = "nosniff"
	fetchSiteCrossSite               = "cross-site"
	headerCacheControl               = "Cache-Control"
	headerContentSecurityPolicy      = "Content-Security-Policy"
	headerContentTypeOptions         = "X-Content-Type-Options"
	headerFetchSite                  = "Sec-Fetch-Site"
	headerOrigin                     = "Origin"
	headerReferrerPolicy             = "Referrer-Policy"
	internalErrorMessage             = "Внутренняя ошибка. Попробуйте снова"
	maxChatMessageRunes              = 3000
	modelUnavailableMessage          = "Не удалось получить ответ модели. Попробуйте снова"
	referrerSameOrigin               = "same-origin"
	roleAssistant                    = "assistant"
	roleUser                         = "user"
	routeAPIPrefix                   = "/api/"
	routeDELETEAPISession            = "DELETE /api/session"
	routeGETAPIConfig                = "GET /api/config"
	routeGETAPIMessages              = "GET /api/messages"
	routeGETAPIPlan                  = "GET /api/plan"
	routeGETAPIProfiles              = "GET /api/profiles"
	routeGETAPIRequests              = "GET /api/requests"
	routeGETAPIResources             = "GET /api/resources"
	routeGETAPISession               = "GET /api/session"
	routeGETHealthz                  = "GET /healthz"
	routePOSTAPIChat                 = "POST /api/chat"
	routePOSTAPIDemoRequestsIDStatus = "POST /api/demo/requests/{id}/status"
	routePOSTAPIDrafts               = "POST /api/drafts"
	routePOSTAPIDraftsIDConfirm      = "POST /api/drafts/{id}/confirm"
	routePOSTAPIRequestsIDRefresh    = "POST /api/requests/{id}/refresh"
	routePOSTAPISession              = "POST /api/session"
	routePOSTAPITasksIDComplete      = "POST /api/tasks/{id}/complete"
	selectProfileMessage             = "Выберите тестовый профиль"
	sessionCookieName                = "onbo_session"
	sessionExpiredMessage            = "Сессия истекла. Выберите профиль снова"
)

type Server struct {
	store    *Store
	sessions Sessions
	agent    core.ChatAgent
	config   Config
	mux      *http.ServeMux
}
type userKey struct{}
type requestLimitKey struct{}

func NewServer(store *Store, sessions Sessions, agent core.ChatAgent, settings Config) http.Handler {
	defaults := config.DefaultApp()
	if settings.API.HealthTimeout == 0 {
		settings.API.HealthTimeout = defaults.API.HealthTimeout
	}
	if settings.API.ChatTimeout == 0 {
		settings.API.ChatTimeout = defaults.API.ChatTimeout
	}
	if settings.API.MaxRequestBytes == 0 {
		settings.API.MaxRequestBytes = defaults.API.MaxRequestBytes
	}
	if settings.SessionTTL == 0 {
		settings.SessionTTL = defaults.Redis.SessionTTL
	}
	server := &Server{store: store, sessions: sessions, agent: agent, config: settings, mux: http.NewServeMux()}
	server.mux.HandleFunc(routeGETHealthz, server.healthHandler)
	server.mux.HandleFunc(routeGETAPIProfiles, func(responseWriter http.ResponseWriter, request *http.Request) {
		if !server.config.DemoMode {
			writeErrorResponse(responseWriter, ErrForbidden)
			return
		}
		writeJSONResponse(responseWriter, http.StatusOK, Profiles)
	})
	server.mux.HandleFunc(routeGETAPIConfig, func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, map[string]any{"demoMode": server.config.DemoMode, "llmMode": server.config.LLMMode})
	})
	server.mux.HandleFunc(routePOSTAPISession, server.createSessionHandler)
	server.mux.HandleFunc(routeGETAPISession, server.requireSession(func(responseWriter http.ResponseWriter, request *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, authenticatedUser(request))
	}))
	server.mux.HandleFunc(routeDELETEAPISession, server.requireSession(server.deleteSessionHandler))
	server.mux.HandleFunc(routeGETAPIPlan, server.requireSession(func(responseWriter http.ResponseWriter, request *http.Request) {
		onboardingPlan, err := server.store.GetOnboardingPlan(request.Context(), authenticatedUser(request))
		writeOperationResponse(responseWriter, onboardingPlan, err)
	}))
	server.mux.HandleFunc(routePOSTAPITasksIDComplete, server.requireSession(func(responseWriter http.ResponseWriter, request *http.Request) {
		var body struct{}
		if !readJSONRequest(responseWriter, request, &body) {
			return
		}
		responseValue, err := server.store.CompleteTask(request.Context(), authenticatedUser(request), request.PathValue("id"))
		writeOperationResponse(responseWriter, responseValue, err)
	}))
	server.mux.HandleFunc(routeGETAPIResources, server.requireSession(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseValue, err := server.store.ListAccessResources(request.Context(), authenticatedUser(request))
		writeOperationResponse(responseWriter, responseValue, err)
	}))
	server.mux.HandleFunc(routeGETAPIRequests, server.requireSession(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseValue, err := server.store.ListAccessRequests(request.Context(), authenticatedUser(request))
		writeOperationResponse(responseWriter, responseValue, err)
	}))
	server.mux.HandleFunc(routeGETAPIMessages, server.requireSession(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseValue, err := server.store.ListChatMessages(request.Context(), authenticatedUser(request))
		writeOperationResponse(responseWriter, responseValue, err)
	}))
	server.mux.HandleFunc(routePOSTAPIDrafts, server.requireSession(server.createAccessDraftHandler))
	server.mux.HandleFunc(routePOSTAPIDraftsIDConfirm, server.requireSession(server.confirmAccessDraftHandler))
	server.mux.HandleFunc(routePOSTAPIRequestsIDRefresh, server.requireSession(func(responseWriter http.ResponseWriter, request *http.Request) {
		var requestBody struct{}
		if !readJSONRequest(responseWriter, request, &requestBody) {
			return
		}
		responseValue, err := server.store.RefreshAccessRequestStatus(request.Context(), authenticatedUser(request), request.PathValue("id"))
		writeOperationResponse(responseWriter, responseValue, err)
	}))
	server.mux.HandleFunc(routePOSTAPIDemoRequestsIDStatus, server.requireSession(server.simulateAccessRequestStatusHandler))
	server.mux.HandleFunc(routePOSTAPIChat, server.requireSession(server.chatHandler))
	server.mux.HandleFunc(routeAPIPrefix, func(responseWriter http.ResponseWriter, request *http.Request) {
		writeErrorResponse(responseWriter, ErrNotFound)
	})
	server.mux.HandleFunc("/", server.serveFrontend)
	return server
}
func (server *Server) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	responseWriter.Header().Set(headerContentTypeOptions, contentTypeNoSniff)
	responseWriter.Header().Set(headerReferrerPolicy, referrerSameOrigin)
	responseWriter.Header().Set(headerContentSecurityPolicy, contentSecurityPolicy)
	if strings.HasPrefix(request.URL.Path, routeAPIPrefix) {
		responseWriter.Header().Set(headerCacheControl, cacheNoStore)
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead && request.Method != http.MethodOptions {
		if request.Header.Get(headerFetchSite) == fetchSiteCrossSite {
			writeErrorResponse(responseWriter, ErrForbidden)
			return
		}
		if raw := request.Header.Get(headerOrigin); raw != "" {
			parsedURL, err := url.Parse(raw)
			scheme := "http"
			if request.TLS != nil {
				scheme = "https"
			}
			if err != nil || parsedURL.Scheme != scheme || parsedURL.Host != request.Host || parsedURL.Path != "" || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
				writeErrorResponse(responseWriter, ErrForbidden)
				return
			}
		}
	}
	server.mux.ServeHTTP(responseWriter, request.WithContext(context.WithValue(request.Context(), requestLimitKey{}, server.config.API.MaxRequestBytes)))
}
func (server *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(sessionCookieName)
		if err != nil {
			writeJSONResponse(responseWriter, http.StatusUnauthorized, map[string]string{"error": selectProfileMessage})
			return
		}
		userID, err := server.sessions.GetSessionUserID(request.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, ErrForbidden) {
				writeJSONResponse(responseWriter, http.StatusUnauthorized, map[string]string{"error": sessionExpiredMessage})
			} else {
				writeErrorResponse(responseWriter, ErrUnavailable)
			}
			return
		}
		user, ok := FindProfile(userID)
		if !ok {
			writeErrorResponse(responseWriter, ErrForbidden)
			return
		}
		next(responseWriter, request.WithContext(context.WithValue(request.Context(), userKey{}, user)))
	}
}
func authenticatedUser(request *http.Request) core.User {
	return request.Context().Value(userKey{}).(core.User)
}
func (server *Server) createSessionHandler(responseWriter http.ResponseWriter, request *http.Request) {
	if !server.config.DemoMode {
		writeErrorResponse(responseWriter, ErrForbidden)
		return
	}
	var requestBody struct {
		ProfileID string `json:"profileId"`
	}
	if !readJSONRequest(responseWriter, request, &requestBody) {
		return
	}
	user, ok := FindProfile(requestBody.ProfileID)
	if !ok {
		writeErrorResponse(responseWriter, ErrInvalid)
		return
	}
	token, err := server.sessions.CreateSession(request.Context(), user.ID)
	if err != nil {
		writeErrorResponse(responseWriter, ErrUnavailable)
		return
	}
	if old, err := request.Cookie(sessionCookieName); err == nil {
		if err = server.sessions.DeleteSession(request.Context(), old.Value); err != nil {
			_ = server.sessions.DeleteSession(request.Context(), token)
			writeErrorResponse(responseWriter, ErrUnavailable)
			return
		}
	}
	http.SetCookie(responseWriter, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", MaxAge: int(server.config.SessionTTL / time.Second), HttpOnly: true, Secure: request.TLS != nil, SameSite: http.SameSiteLaxMode})
	writeJSONResponse(responseWriter, http.StatusOK, user)
}
func (server *Server) deleteSessionHandler(responseWriter http.ResponseWriter, request *http.Request) {
	cookie, _ := request.Cookie(sessionCookieName)
	if err := server.sessions.DeleteSession(request.Context(), cookie.Value); err != nil {
		writeErrorResponse(responseWriter, ErrUnavailable)
		return
	}
	http.SetCookie(responseWriter, &http.Cookie{Name: sessionCookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: request.TLS != nil, SameSite: http.SameSiteLaxMode})
	responseWriter.WriteHeader(http.StatusNoContent)
}
func (server *Server) healthHandler(responseWriter http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), server.config.API.HealthTimeout)
	defer cancel()
	if err := server.store.Pool.Ping(ctx); err != nil {
		writeErrorResponse(responseWriter, ErrUnavailable)
		return
	}
	if err := server.sessions.Ping(ctx); err != nil {
		writeErrorResponse(responseWriter, ErrUnavailable)
		return
	}
	writeJSONResponse(responseWriter, http.StatusOK, map[string]string{"status": "ok"})
}
func (server *Server) createAccessDraftHandler(responseWriter http.ResponseWriter, request *http.Request) {
	var requestBody struct {
		ResourceID string `json:"resourceId"`
		Reason     string `json:"reason"`
	}
	if !readJSONRequest(responseWriter, request, &requestBody) {
		return
	}
	accessDraft, err := server.store.CreateAccessDraft(request.Context(), authenticatedUser(request), requestBody.ResourceID, requestBody.Reason)
	writeOperationResponse(responseWriter, accessDraft, err)
}
func (server *Server) confirmAccessDraftHandler(responseWriter http.ResponseWriter, request *http.Request) {
	var requestBody struct {
		Confirmed bool `json:"confirmed"`
	}
	if !readJSONRequest(responseWriter, request, &requestBody) {
		return
	}
	responseValue, err := server.store.ConfirmAndSubmitAccessDraft(request.Context(), authenticatedUser(request), request.PathValue("id"), requestBody.Confirmed)
	writeOperationResponse(responseWriter, responseValue, err)
}
func (server *Server) simulateAccessRequestStatusHandler(responseWriter http.ResponseWriter, request *http.Request) {
	if !server.config.DemoMode {
		writeErrorResponse(responseWriter, ErrForbidden)
		return
	}
	var requestBody struct {
		Status string `json:"status"`
	}
	if !readJSONRequest(responseWriter, request, &requestBody) {
		return
	}
	responseValue, err := server.store.SimulateAccessRequestStatus(request.Context(), authenticatedUser(request), request.PathValue("id"), requestBody.Status)
	writeOperationResponse(responseWriter, responseValue, err)
}
func (server *Server) chatHandler(responseWriter http.ResponseWriter, request *http.Request) {
	var requestBody struct {
		Message string `json:"message"`
	}
	if !readJSONRequest(responseWriter, request, &requestBody) {
		return
	}
	requestBody.Message = strings.TrimSpace(requestBody.Message)
	if requestBody.Message == "" || len([]rune(requestBody.Message)) > maxChatMessageRunes {
		writeErrorResponse(responseWriter, ErrInvalid)
		return
	}
	user := authenticatedUser(request)
	allowed, err := server.sessions.ConsumeChatQuota(request.Context(), user.ID)
	if err != nil {
		writeErrorResponse(responseWriter, ErrUnavailable)
		return
	}
	if !allowed {
		writeJSONResponse(responseWriter, http.StatusTooManyRequests, map[string]string{"error": chatRateLimitMessage})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), server.config.API.ChatTimeout)
	defer cancel()
	history, err := server.store.ListChatMessages(ctx, user)
	if err != nil {
		writeErrorResponse(responseWriter, err)
		return
	}
	reply, err := server.agent.GenerateReply(ctx, user, history, requestBody.Message)
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			code = http.StatusGatewayTimeout
		}
		writeJSONResponse(responseWriter, code, map[string]string{"error": modelUnavailableMessage})
		return
	}
	if reply.Sources == nil {
		reply.Sources = []core.Source{}
	}
	err = server.store.SaveChatMessages(ctx, user, core.Message{Role: roleUser, Text: requestBody.Message}, core.Message{Role: roleAssistant, Text: reply.Message, Sources: reply.Sources, Draft: reply.Draft})
	if err != nil {
		writeErrorResponse(responseWriter, err)
		return
	}
	writeJSONResponse(responseWriter, http.StatusOK, reply)
}
func (server *Server) serveFrontend(responseWriter http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		responseWriter.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if server.config.StaticDir == "" {
		http.NotFound(responseWriter, request)
		return
	}
	clean := filepath.Clean("/" + request.URL.Path)
	target := filepath.Join(server.config.StaticDir, clean)
	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		target = filepath.Join(server.config.StaticDir, "index.html")
	}
	http.ServeFile(responseWriter, request, target)
}
func readJSONRequest(responseWriter http.ResponseWriter, request *http.Request, requestBody any) bool {
	limit := config.DefaultApp().API.MaxRequestBytes
	if requestLimit, ok := request.Context().Value(requestLimitKey{}).(int64); ok {
		limit = requestLimit
	}
	request.Body = http.MaxBytesReader(responseWriter, request.Body, limit)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(requestBody); err != nil {
		writeErrorResponse(responseWriter, ErrInvalid)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeErrorResponse(responseWriter, ErrInvalid)
		return false
	}
	return true
}
func writeJSONResponse(responseWriter http.ResponseWriter, code int, responseValue any) {
	responseWriter.Header().Set(core.HeaderContentType, core.ContentTypeJSONUTF8)
	responseWriter.WriteHeader(code)
	_ = json.NewEncoder(responseWriter).Encode(responseValue)
}
func writeOperationResponse(responseWriter http.ResponseWriter, responseValue any, err error) {
	if err != nil {
		writeErrorResponse(responseWriter, err)
		return
	}
	writeJSONResponse(responseWriter, http.StatusOK, responseValue)
}
func writeErrorResponse(responseWriter http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	message := internalErrorMessage
	switch {
	case errors.Is(err, ErrNotFound):
		code = http.StatusNotFound
		message = err.Error()
	case errors.Is(err, ErrForbidden):
		code = http.StatusForbidden
		message = err.Error()
	case errors.Is(err, ErrInvalid):
		code = http.StatusBadRequest
		message = err.Error()
	case errors.Is(err, ErrUnavailable):
		code = http.StatusServiceUnavailable
		message = err.Error()
	}
	writeJSONResponse(responseWriter, code, map[string]string{"error": message})
}
