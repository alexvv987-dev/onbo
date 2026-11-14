package core

import "context"

const (
	AnalyticsProfileID   = "intern-analytics"
	ContentTypeJSON      = "application/json"
	ContentTypeJSONUTF8  = "application/json; charset=utf-8"
	HeaderContentType    = "Content-Type"
	HeaderMockToken      = "X-Mock-Token"
	PlatformProfileID    = "intern-platform"
	PlatformTeam         = "Платформа"
	RequestGranted       = "granted"
	RequestPending       = "pending"
	RequestRejected      = "rejected"
	RequestSubmitting    = "submitting"
	TaskAccess           = "access"
	TaskBlocked          = "blocked"
	TaskDone             = "done"
	TaskFirstAssignment  = "first-task"
	TaskInstructions     = "instructions"
	TaskTeam             = "team"
	TaskTodo             = "todo"
	TrainingResourceID   = "training-repo"
	TrainingResourceName = "Учебный репозиторий"
)

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Team string `json:"team"`
}
type Task struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Manual      bool   `json:"manual"`
}
type Plan struct {
	Tasks    []Task `json:"tasks"`
	NextStep string `json:"nextStep"`
}
type Source struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
	Label   string `json:"label"`
}
type Resource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Draft struct {
	ID           string `json:"id"`
	ResourceID   string `json:"resourceId"`
	ResourceName string `json:"resourceName"`
	Reason       string `json:"reason"`
}
type AccessRequest struct {
	ID           string `json:"id"`
	DraftID      string `json:"draftId"`
	ResourceID   string `json:"resourceId"`
	ResourceName string `json:"resourceName"`
	Status       string `json:"status"`
	Message      string `json:"message"`
}
type Message struct {
	Role    string   `json:"role"`
	Text    string   `json:"text"`
	Sources []Source `json:"sources,omitempty"`
	Draft   *Draft   `json:"draft,omitempty"`
}
type ChatReply struct {
	Message string   `json:"message"`
	Sources []Source `json:"sources"`
	Draft   *Draft   `json:"draft,omitempty"`
}

// Tools always receive the authenticated user from the server, never from model arguments.
type Tools interface {
	GetOnboardingPlan(context.Context, User) (Plan, error)
	SearchKnowledge(context.Context, User, string) ([]Source, error)
	ListAccessResources(context.Context, User) ([]Resource, error)
	CreateAccessDraft(context.Context, User, string, string) (Draft, error)
	ListAccessRequests(context.Context, User) ([]AccessRequest, error)
}
type ChatAgent interface {
	GenerateReply(context.Context, User, []Message, string) (ChatReply, error)
}
