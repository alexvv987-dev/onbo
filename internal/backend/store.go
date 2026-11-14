package backend

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"onbo/internal/config"
	"onbo/internal/core"
)

const (
	operationColumns         = "id,draft_id,user_id,resource_id,external_id,status,message"
	accessPendingNextStep    = "Дождитесь решения по заявке и обновите её статус."
	accessRejectedNextStep   = "Заявка отклонена. Уточните причину у наставника."
	accessTaskDescription    = "Создайте заявку и дождитесь подтверждения выдачи доступа."
	accessTaskTitle          = "Получить доступ к учебному репозиторию"
	blockedNextStep          = "Обратитесь к наставнику своей команды за следующим шагом."
	firstAssignmentTaskIndex = 3
	firstTaskDescription     = "После выдачи доступа прочитайте README и обсудите задачу с наставником."
	firstTaskTitle           = "Приступить к первой задаче"
	identifierByteCount      = 16
	initializeSchemaSQL      = `CREATE TABLE IF NOT EXISTS onbo_progress(user_id text NOT NULL,task_id text NOT NULL,PRIMARY KEY(user_id,task_id));
 CREATE TABLE IF NOT EXISTS onbo_documents(id text PRIMARY KEY,team text NOT NULL,title text NOT NULL,content text NOT NULL);
 CREATE TABLE IF NOT EXISTS onbo_drafts(id text PRIMARY KEY,user_id text NOT NULL,resource_id text NOT NULL,reason text NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
 CREATE TABLE IF NOT EXISTS onbo_operations(id text PRIMARY KEY,draft_id text UNIQUE NOT NULL,user_id text NOT NULL,resource_id text NOT NULL,external_id text NOT NULL DEFAULT '',status text NOT NULL DEFAULT '` +
		core.RequestSubmitting + `',message text NOT NULL DEFAULT 'Результат ещё не подтверждён. Повторите проверку.',created_at timestamptz NOT NULL DEFAULT now());
 CREATE TABLE IF NOT EXISTS onbo_messages(id bigserial PRIMARY KEY,user_id text NOT NULL,body jsonb NOT NULL);
 INSERT INTO onbo_documents(id,team,title,content) VALUES
 ('team-platform','Платформа','Знакомство с командой платформы','Тестовая инструкция, не регламент Авито. Ваш наставник — Алексей (синтетический профиль), канал помощи — #platform-interns. Начните со знакомства с командой и изучения процесса разработки.'),
 ('workflow','Платформа','Как подготовиться к первой задаче','Тестовая инструкция, не регламент Авито. Для подготовки окружения прочитайте инструкцию и получите доступ к учебному репозиторию training-repo. Создайте заявку с причиной «Подготовка к первой учебной задаче», дождитесь статуса «Доступ выдан», затем переходите к первой задаче.'),
 ('access-policy','Платформа','Порядок получения доступа','Тестовая инструкция, не регламент Авито. Ресурс training-repo — учебный репозиторий. Помощник готовит черновик заявки; проверьте ресурс и причину, затем нажмите «Подтвердить и отправить». Pending означает ожидание, granted — доступ выдан, rejected — отказ. При отказе обратитесь к наставнику в #platform-interns.'),
 ('analytics-secret','Аналитика','Внутренняя инструкция команды аналитики','Тестовая инструкция, не регламент Авито. Учебный код аналитики ALPHA-ANALYTICS-SECRET доступен только команде аналитики. Наставник — Марина, канал #analytics-interns.'),
 ('common','*','О помощнике Онбо','Тестовые данные. Онбо помогает освоиться в команде, разобраться с инструкциями и запросить доступ. Помощник не заменяет наставника. Отмечайте ознакомление в плане; статусы доступа приходят из сервиса.'),
 ('first-task','Платформа','Первая учебная задача','Тестовая инструкция. После выдачи доступа откройте учебный репозиторий, прочитайте README и обсудите первую учебную задачу с наставником. Отметьте начало задачи в плане самостоятельно.')
 ON CONFLICT(id) DO NOTHING;`
	insertAccessDraftQuery         = "INSERT INTO onbo_drafts(id,user_id,resource_id,reason) VALUES($1,$2,$3,$4)"
	insertAccessOperationQuery     = "INSERT INTO onbo_operations(id,draft_id,user_id,resource_id) VALUES($1,$2,$3,$4) ON CONFLICT(draft_id) DO NOTHING"
	insertChatMessageQuery         = "INSERT INTO onbo_messages(user_id,body) VALUES($1,$2)"
	insertTaskProgressQuery        = "INSERT INTO onbo_progress(user_id,task_id) VALUES($1,$2) ON CONFLICT DO NOTHING"
	instructionsTaskDescription    = "Разберитесь с процессом подготовки к первой задаче."
	instructionsTaskTitle          = "Изучить рабочую инструкцию"
	introductoryTaskCount          = 2
	maxAccessReasonRunes           = 500
	maxAccessServiceResponseBytes  = 1 << 20
	maxSearchQueryBytes            = 1500
	planCompletedMessage           = "Все шаги выполнены. Обсудите дальнейший план с наставником."
	randomSourceUnavailableMessage = "secure random source unavailable"
	accessRequestsPath             = "/requests"
	accessRequestPathPrefix        = "/requests/"
	routeRequestsByKey             = "/requests/by-key/"
	routeStatus                    = "/status"
	saveAccessServiceResultQuery   = `UPDATE onbo_operations SET
 external_id=CASE WHEN status IN ('` + core.RequestGranted + `','` + core.RequestRejected + `') THEN external_id ELSE $1 END,
 status=CASE WHEN status IN ('` + core.RequestGranted + `','` + core.RequestRejected + `') THEN status ELSE $2 END,
 message=CASE WHEN status IN ('` + core.RequestGranted + `','` + core.RequestRejected + `') THEN message ELSE $3 END
 WHERE id=$4 RETURNING ` + operationColumns
	searchKnowledgeQuery              = "SELECT id,title,content FROM onbo_documents WHERE (team=$1 OR team='*') AND (to_tsvector('russian',title||' '||content) @@ websearch_to_tsquery('russian',$2) OR title ILIKE '%'||$2||'%') ORDER BY ts_rank(to_tsvector('russian',title||' '||content),websearch_to_tsquery('russian',$2)) DESC,id LIMIT 4"
	selectAccessRequestsQuery         = "SELECT " + operationColumns + " FROM onbo_operations WHERE user_id=$1 ORDER BY created_at DESC,id DESC"
	selectChatMessagesQuery           = "SELECT body FROM (SELECT id,body FROM onbo_messages WHERE user_id=$1 ORDER BY id DESC LIMIT 100) m ORDER BY id"
	selectDraftResourceQuery          = "SELECT resource_id FROM onbo_drafts WHERE id=$1 AND user_id=$2"
	selectOperationByDraftQuery       = "SELECT " + operationColumns + " FROM onbo_operations WHERE draft_id=$1 AND user_id=$2"
	selectOperationForSimulationQuery = "SELECT " + operationColumns + " FROM onbo_operations WHERE (id=$1 OR external_id=$1) AND user_id=$2"
	selectOwnedAccessOperationQuery   = "SELECT " + operationColumns + " FROM onbo_operations WHERE (id=$1 OR external_id=$1) AND user_id=$2"
	selectTaskProgressQuery           = "SELECT task_id FROM onbo_progress WHERE user_id=$1"
	syntheticSourceLabel              = "Тестовая инструкция · не регламент Авито"
	teamTaskDescription               = "Узнайте, кто ваш наставник и куда обращаться за помощью."
	teamTaskTitle                     = "Познакомиться с командой"
	trainingResourceDescription       = "Тестовый ресурс для подготовки к первой задаче."
)

var Profiles = []core.User{
	{ID: core.PlatformProfileID, Name: "Саша Иванова", Team: core.PlatformTeam},
	{ID: core.AnalyticsProfileID, Name: "Миша Петров", Team: "Аналитика"},
}

func FindProfile(id string) (core.User, bool) {
	for _, user := range Profiles {
		if user.ID == id {
			return user, true
		}
	}
	return core.User{}, false
}
func generateIdentifier() string {
	identifierBytes := make([]byte, identifierByteCount)
	if _, err := rand.Read(identifierBytes); err != nil {
		panic(randomSourceUnavailableMessage)
	}
	return hex.EncodeToString(identifierBytes)
}

type Store struct {
	Pool               *pgxpool.Pool
	MockURL, MockToken string
	Client             *http.Client
}

func NewStore(ctx context.Context, databaseURL, mockURL, mockToken string, options ...config.AccessService) (*Store, error) {
	settings := config.DefaultApp().Mock
	if len(options) > 0 {
		settings = options[0]
	}
	databasePool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	store := &Store{Pool: databasePool, MockURL: strings.TrimRight(mockURL, "/"), MockToken: mockToken, Client: &http.Client{Timeout: settings.Timeout}}
	if err = databasePool.Ping(ctx); err != nil {
		databasePool.Close()
		return nil, err
	}
	if err = store.initializeSchema(ctx); err != nil {
		databasePool.Close()
		return nil, err
	}
	return store, nil
}
func (store *Store) Close() { store.Pool.Close() }
func (store *Store) initializeSchema(ctx context.Context) error {
	_, err := store.Pool.Exec(ctx, initializeSchemaSQL)
	return err
}

func (store *Store) GetOnboardingPlan(ctx context.Context, user core.User) (core.Plan, error) {
	user, ok := FindProfile(user.ID)
	if !ok {
		return core.Plan{}, ErrForbidden
	}
	rows, err := store.Pool.Query(ctx, selectTaskProgressQuery, user.ID)
	if err != nil {
		return core.Plan{}, err
	}
	done := map[string]bool{}
	for rows.Next() {
		var taskID string
		if err = rows.Scan(&taskID); err != nil {
			rows.Close()
			return core.Plan{}, err
		}
		done[taskID] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return core.Plan{}, err
	}
	accessRequests, err := store.ListAccessRequests(ctx, user)
	if err != nil {
		return core.Plan{}, err
	}
	status := core.TaskTodo
	if user.Team != core.PlatformTeam {
		status = core.TaskBlocked
	}
	if len(accessRequests) > 0 {
		status = accessRequests[0].Status
		if status == core.RequestGranted {
			status = core.TaskDone
		}
		if status == core.RequestSubmitting {
			status = core.RequestPending
		}
	}

	for _, request := range accessRequests {
		if request.Status == core.RequestGranted {
			status = core.TaskDone
			break
		}
	}
	onboardingPlan := core.Plan{Tasks: []core.Task{
		{ID: core.TaskTeam, Title: teamTaskTitle, Description: teamTaskDescription, Status: core.TaskTodo, Manual: true},
		{ID: core.TaskInstructions, Title: instructionsTaskTitle, Description: instructionsTaskDescription, Status: core.TaskTodo, Manual: true},
		{ID: core.TaskAccess, Title: accessTaskTitle, Description: accessTaskDescription, Status: status, Manual: false},
		{ID: core.TaskFirstAssignment, Title: firstTaskTitle, Description: firstTaskDescription, Status: core.TaskBlocked, Manual: true},
	}}
	for itemIndex := 0; itemIndex < introductoryTaskCount; itemIndex++ {
		if done[onboardingPlan.Tasks[itemIndex].ID] {
			onboardingPlan.Tasks[itemIndex].Status = core.TaskDone
		}
	}
	if status == core.TaskDone {
		onboardingPlan.Tasks[firstAssignmentTaskIndex].Status = core.TaskTodo
		if done[core.TaskFirstAssignment] {
			onboardingPlan.Tasks[firstAssignmentTaskIndex].Status = core.TaskDone
		}
	}
	onboardingPlan.NextStep = planCompletedMessage
	for _, task := range onboardingPlan.Tasks {
		if task.Status != core.TaskDone {
			switch task.Status {
			case core.RequestPending:
				onboardingPlan.NextStep = accessPendingNextStep
			case core.RequestRejected:
				onboardingPlan.NextStep = accessRejectedNextStep
			case core.TaskBlocked:
				onboardingPlan.NextStep = blockedNextStep
			default:
				onboardingPlan.NextStep = task.Title
			}
			break
		}
	}
	return onboardingPlan, nil
}
func (store *Store) CompleteTask(ctx context.Context, user core.User, taskID string) (core.Plan, error) {
	onboardingPlan, err := store.GetOnboardingPlan(ctx, user)
	if err != nil {
		return onboardingPlan, err
	}
	found := false
	for _, task := range onboardingPlan.Tasks {
		if task.ID == taskID {
			found = true
			if !task.Manual || task.Status == core.TaskBlocked {
				return onboardingPlan, ErrForbidden
			}
		}
	}
	if !found {
		return onboardingPlan, ErrNotFound
	}
	_, err = store.Pool.Exec(ctx, insertTaskProgressQuery, user.ID, taskID)
	if err != nil {
		return onboardingPlan, err
	}
	return store.GetOnboardingPlan(ctx, user)
}
func (store *Store) SearchKnowledge(ctx context.Context, user core.User, query string) ([]core.Source, error) {
	user, ok := FindProfile(user.ID)
	if !ok {
		return nil, ErrForbidden
	}
	query = strings.TrimSpace(query)
	if query == "" || len(query) > maxSearchQueryBytes {
		return nil, ErrInvalid
	}
	rows, err := store.Pool.Query(ctx, searchKnowledgeQuery, user.Team, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []core.Source{}
	for rows.Next() {
		var source core.Source
		if err = rows.Scan(&source.ID, &source.Title, &source.Excerpt); err != nil {
			return nil, err
		}
		source.Label = syntheticSourceLabel
		sources = append(sources, source)
	}
	return sources, rows.Err()
}
func (store *Store) ListAccessResources(ctx context.Context, user core.User) ([]core.Resource, error) {
	user, ok := FindProfile(user.ID)
	if !ok {
		return nil, ErrForbidden
	}
	if user.Team != core.PlatformTeam {
		return []core.Resource{}, nil
	}
	return []core.Resource{{ID: core.TrainingResourceID, Name: core.TrainingResourceName, Description: trainingResourceDescription}}, nil
}
func (store *Store) CreateAccessDraft(ctx context.Context, user core.User, resourceID, reason string) (core.Draft, error) {
	resources, err := store.ListAccessResources(ctx, user)
	if err != nil {
		return core.Draft{}, err
	}
	allowed := false
	name := ""
	for _, request := range resources {
		if request.ID == resourceID {
			allowed = true
			name = request.Name
		}
	}
	if !allowed {
		return core.Draft{}, ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > maxAccessReasonRunes {
		return core.Draft{}, ErrInvalid
	}
	accessDraft := core.Draft{ID: generateIdentifier(), ResourceID: resourceID, ResourceName: name, Reason: reason}
	_, err = store.Pool.Exec(ctx, insertAccessDraftQuery, accessDraft.ID, user.ID, accessDraft.ResourceID, accessDraft.Reason)
	return accessDraft, err
}

type operation struct{ ID, DraftID, UserID, ResourceID, ExternalID, Status, Message string }

func (accessOperation operation) toAccessRequest() core.AccessRequest {
	requestID := accessOperation.ID
	if accessOperation.ExternalID != "" {
		requestID = accessOperation.ExternalID
	}
	return core.AccessRequest{ID: requestID, DraftID: accessOperation.DraftID, ResourceID: accessOperation.ResourceID, ResourceName: core.TrainingResourceName, Status: accessOperation.Status, Message: accessOperation.Message}
}
func scanAccessOperation(row pgx.Row) (operation, error) {
	var accessOperation operation
	err := row.Scan(&accessOperation.ID, &accessOperation.DraftID, &accessOperation.UserID, &accessOperation.ResourceID, &accessOperation.ExternalID, &accessOperation.Status, &accessOperation.Message)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return accessOperation, err
}

func (store *Store) ListAccessRequests(ctx context.Context, user core.User) ([]core.AccessRequest, error) {
	if _, ok := FindProfile(user.ID); !ok {
		return nil, ErrForbidden
	}
	rows, err := store.Pool.Query(ctx, selectAccessRequestsQuery, user.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accessRequests := []core.AccessRequest{}
	for rows.Next() {
		accessOperation, err := scanAccessOperation(rows)
		if err != nil {
			return nil, err
		}
		accessRequests = append(accessRequests, accessOperation.toAccessRequest())
	}
	return accessRequests, rows.Err()
}
func (store *Store) ConfirmAndSubmitAccessDraft(ctx context.Context, user core.User, draftID string, confirmed bool) (core.AccessRequest, error) {
	if !confirmed {
		return core.AccessRequest{}, ErrInvalid
	}
	if _, ok := FindProfile(user.ID); !ok {
		return core.AccessRequest{}, ErrForbidden
	}
	var resource string
	err := store.Pool.QueryRow(ctx, selectDraftResourceQuery, draftID, user.ID).Scan(&resource)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.AccessRequest{}, ErrNotFound
	}
	if err != nil {
		return core.AccessRequest{}, err
	}
	resources, err := store.ListAccessResources(ctx, user)
	if err != nil {
		return core.AccessRequest{}, err
	}
	if len(resources) == 0 {
		return core.AccessRequest{}, ErrForbidden
	}
	_, err = store.Pool.Exec(ctx, insertAccessOperationQuery, generateIdentifier(), draftID, user.ID, resource)
	if err != nil {
		return core.AccessRequest{}, err
	}
	accessOperation, err := scanAccessOperation(store.Pool.QueryRow(ctx, selectOperationByDraftQuery, draftID, user.ID))
	if err != nil {
		return core.AccessRequest{}, err
	}
	if accessOperation.Status != core.RequestSubmitting {
		return accessOperation.toAccessRequest(), nil
	}
	return store.recoverOrCreateAccessRequest(ctx, accessOperation, true)
}
func (store *Store) RefreshAccessRequestStatus(ctx context.Context, user core.User, requestID string) (core.AccessRequest, error) {
	accessOperation, err := scanAccessOperation(store.Pool.QueryRow(ctx, selectOwnedAccessOperationQuery, requestID, user.ID))
	if err != nil {
		return core.AccessRequest{}, err
	}
	return store.recoverOrCreateAccessRequest(ctx, accessOperation, accessOperation.Status == core.RequestSubmitting)
}

type remoteRequest struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (store *Store) callAccessService(ctx context.Context, method, path string, body any) (remoteRequest, int, error) {
	var requestBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return remoteRequest{}, 0, err
		}
		requestBody = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, store.MockURL+path, requestBody)
	if err != nil {
		return remoteRequest{}, 0, ErrUnavailable
	}
	request.Header.Set(core.HeaderMockToken, store.MockToken)
	request.Header.Set(core.HeaderContentType, core.ContentTypeJSON)
	response, err := store.Client.Do(request)
	if err != nil {
		return remoteRequest{}, 0, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return remoteRequest{}, response.StatusCode, ErrUnavailable
	}
	var accessServiceResult remoteRequest
	if err = json.NewDecoder(io.LimitReader(response.Body, maxAccessServiceResponseBytes)).Decode(&accessServiceResult); err != nil {
		return accessServiceResult, response.StatusCode, ErrUnavailable
	}
	if accessServiceResult.ID == "" || (accessServiceResult.Status != core.RequestPending && accessServiceResult.Status != core.RequestGranted && accessServiceResult.Status != core.RequestRejected) {
		return accessServiceResult, response.StatusCode, ErrUnavailable
	}
	return accessServiceResult, response.StatusCode, nil
}
func (store *Store) recoverOrCreateAccessRequest(ctx context.Context, accessOperation operation, create bool) (core.AccessRequest, error) {
	accessRequest, status, err := store.callAccessService(ctx, http.MethodGet, routeRequestsByKey+url.PathEscape(accessOperation.ID), nil)
	if status == http.StatusNotFound && create {
		accessRequest, _, err = store.callAccessService(ctx, http.MethodPost, accessRequestsPath, map[string]string{"key": accessOperation.ID, "userId": accessOperation.UserID, "resourceId": accessOperation.ResourceID})
	}
	if err != nil {
		return accessOperation.toAccessRequest(), ErrUnavailable
	}
	return store.saveAccessServiceResult(ctx, accessOperation, accessRequest)
}
func (store *Store) saveAccessServiceResult(ctx context.Context, accessOperation operation, request remoteRequest) (core.AccessRequest, error) {
	// A delayed response can arrive after another request stored a terminal result.
	// Preserve the entire terminal snapshot atomically and return the row that won.
	current, err := scanAccessOperation(store.Pool.QueryRow(ctx, saveAccessServiceResultQuery, request.ID, request.Status, request.Message, accessOperation.ID))
	if err != nil {
		return accessOperation.toAccessRequest(), err
	}
	return current.toAccessRequest(), nil
}
func (store *Store) SimulateAccessRequestStatus(ctx context.Context, user core.User, requestID, status string) (core.AccessRequest, error) {
	if status != core.RequestGranted && status != core.RequestRejected {
		return core.AccessRequest{}, ErrInvalid
	}
	accessOperation, err := scanAccessOperation(store.Pool.QueryRow(ctx, selectOperationForSimulationQuery, requestID, user.ID))
	if err != nil {
		return core.AccessRequest{}, err
	}
	if accessOperation.ExternalID == "" {
		return core.AccessRequest{}, ErrInvalid
	}
	accessRequest, _, err := store.callAccessService(ctx, http.MethodPost, accessRequestPathPrefix+url.PathEscape(accessOperation.ExternalID)+routeStatus, map[string]string{"status": status})
	if err != nil {
		return core.AccessRequest{}, err
	}
	return store.saveAccessServiceResult(ctx, accessOperation, accessRequest)
}
func (store *Store) ListChatMessages(ctx context.Context, user core.User) ([]core.Message, error) {
	rows, err := store.Pool.Query(ctx, selectChatMessagesQuery, user.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := []core.Message{}
	for rows.Next() {
		var encodedMessage []byte
		if err = rows.Scan(&encodedMessage); err != nil {
			return nil, err
		}
		var message core.Message
		if err = json.Unmarshal(encodedMessage, &message); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}
func (store *Store) SaveChatMessages(ctx context.Context, user core.User, messages ...core.Message) error {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, message := range messages {
		raw, err := json.Marshal(message)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, insertChatMessageQuery, user.ID, raw); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
