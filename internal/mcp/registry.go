package mcp

import "context"

// Tool describes the local MCP action surface used by the onboarding agent.
// Authorization remains in the existing backend service.
type Tool struct {
	Name        string
	Description string
}

type Registry struct {
	tools []Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: []Tool{
		{Name: "get_onboarding_plan", Description: "Получить план адаптации текущего стажёра"},
		{Name: "search_knowledge", Description: "Найти доступную инструкцию команды"},
		{Name: "list_access_resources", Description: "Показать доступные ресурсы команды"},
		{Name: "draft_access_request", Description: "Подготовить черновик заявки на доступ"},
		{Name: "get_access_request_status", Description: "Проверить статус своей заявки"},
	}}
}

func (registry *Registry) Tools(context.Context) []Tool {
	result := make([]Tool, len(registry.tools))
	copy(result, registry.tools)
	return result
}
