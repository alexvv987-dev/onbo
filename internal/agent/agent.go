// Package agent connects the onboarding application to GigaChat. Tool arguments
// are untrusted model output; the authenticated identity never comes from them.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"onbo/internal/core"
)

const (
	planToolDescription      = "Текущий персональный план, статусы и следующий шаг стажёра."
	searchToolDescription    = "ОБЯЗАТЕЛЬНЫЙ первый инструмент для вопросов о команде, наставнике, к кому обратиться, инструкциях, порядке действий и получения доступа. Вызови сам до любого содержательного ответа, даже если ответ кажется очевидным или основание заявки ещё неизвестно. Возвращает только разрешённые тестовые инструкции."
	searchQueryDescription   = "Короткие ключевые слова на русском: например наставник, первая задача, доступ. Поиск всегда ограничен текущими правами."
	resourcesToolDescription = "Ресурсы, к которым текущий пользователь имеет право запросить доступ."
	draftToolDescription     = "Подготовить неизменяемый черновик заявки. НЕ отправляет заявку и НЕ выдаёт доступ. Требует указанного пользователем ресурса и основания."
	resourceIDDescription    = "Идентификатор разрешённого ресурса из list_access_resources"
	accessReasonDescription  = "Основание заявки от пользователя"
	statusToolDescription    = "Прочитать статус своей заявки. Не изменяет заявку и не выдаёт доступ."
	requestIDDescription     = "Номер своей заявки, либо пустая строка для списка своих заявок"

	draftConfirmationNotice    = "Пользователь должен явно подтвердить черновик кнопкой интерфейса"
	failedToolResultJSON       = `{"error":"Операция недоступна или не выполнена. Не утверждай, что действие выполнено."}`
	maxAccessReasonRunes       = 500
	maxHistoryMessages         = 12
	maxHistoryTextBytes        = 4000
	maxHistoryTextRunes        = 1000
	maxKnowledgeQueryRunes     = 500
	maxRequestIDBytes          = 200
	maxResourceIDRunes         = 100
	maxSources                 = 5
	maxToolArgumentBytes       = 4096
	requestUnavailableMessage  = "Заявка недоступна или не найдена"
	roleAssistant              = "assistant"
	roleUser                   = "user"
	syntheticSourcesNotice     = "Тестовые инструкции, не регламенты Авито"
	toolCreateAccessDraft      = "draft_access_request"
	toolGetAccessRequestStatus = "get_access_request_status"
	toolGetOnboardingPlan      = "get_onboarding_plan"
	toolListAccessResources    = "list_access_resources"
	toolResultErrorFormat      = "%w: результат инструмента"
	toolSearchKnowledge        = "search_knowledge"
)

type functionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type message struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	Name             string        `json:"name,omitempty"`
	FunctionCall     *functionCall `json:"function_call,omitempty"`
	FunctionsStateID string        `json:"functions_state_id,omitempty"`
}

type completion struct {
	Choices []struct {
		Message      message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func toolParameters(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func toolDefinitions() []toolDefinition {
	stringProperty := func(description string) any { return map[string]any{"type": "string", "description": description} }
	return []toolDefinition{
		{toolGetOnboardingPlan, planToolDescription, toolParameters(map[string]any{})},
		{toolSearchKnowledge, searchToolDescription, toolParameters(map[string]any{"query": stringProperty(searchQueryDescription)}, "query")},
		{toolListAccessResources, resourcesToolDescription, toolParameters(map[string]any{})},
		{toolCreateAccessDraft, draftToolDescription, toolParameters(map[string]any{"resourceId": stringProperty(resourceIDDescription), "reason": stringProperty(accessReasonDescription)}, "resourceId", "reason")},
		{toolGetAccessRequestStatus, statusToolDescription, toolParameters(map[string]any{"requestId": stringProperty(requestIDDescription)}, "requestId")},
	}
}

type toolResults struct {
	sources      []core.Source
	draft        *core.Draft
	used         bool
	nonKnowledge bool
}

func decodeToolArguments(raw json.RawMessage, out any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || len(raw) > maxToolArgumentBytes || trimmed[0] != '{' {
		return ErrArguments
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return ErrArguments
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrArguments
	}
	return nil
}

func isValidString(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= limit
}

func executeTool(ctx context.Context, tools core.Tools, user core.User, call *functionCall, state *toolResults) (string, error) {
	if call == nil {
		return "", ErrArguments
	}
	var value any
	var err error
	switch call.Name {
	case toolGetOnboardingPlan:
		if err = decodeToolArguments(call.Arguments, &struct{}{}); err != nil {
			return "", err
		}
		value, err = tools.GetOnboardingPlan(ctx, user)
		state.nonKnowledge = true
	case toolSearchKnowledge:
		var arguments struct {
			Query string `json:"query"`
		}
		if err = decodeToolArguments(call.Arguments, &arguments); err != nil || !isValidString(arguments.Query, maxKnowledgeQueryRunes) {
			return "", ErrArguments
		}
		var found []core.Source
		found, err = tools.SearchKnowledge(ctx, user, strings.TrimSpace(arguments.Query))
		if err == nil {
			if len(found) > maxSources {
				found = found[:maxSources]
			}
			for _, source := range found {
				seen := false
				for _, old := range state.sources {
					if old.ID == source.ID {
						seen = true
						break
					}
				}
				if !seen {
					state.sources = append(state.sources, source)
				}
			}
		}
		value = map[string]any{"sources": found, "notice": syntheticSourcesNotice}
	case toolListAccessResources:
		if err = decodeToolArguments(call.Arguments, &struct{}{}); err != nil {
			return "", err
		}
		value, err = tools.ListAccessResources(ctx, user)
		state.nonKnowledge = true
	case toolCreateAccessDraft:
		var arguments struct {
			ResourceID string `json:"resourceId"`
			Reason     string `json:"reason"`
		}
		if err = decodeToolArguments(call.Arguments, &arguments); err != nil || !isValidString(arguments.ResourceID, maxResourceIDRunes) || !isValidString(arguments.Reason, maxAccessReasonRunes) {
			return "", ErrArguments
		}
		var draft core.Draft
		draft, err = tools.CreateAccessDraft(ctx, user, arguments.ResourceID, strings.TrimSpace(arguments.Reason))
		if err == nil {
			state.draft = &draft
		}
		state.nonKnowledge = true
		value = map[string]any{"draft": draft, "submitted": false, "nextStep": draftConfirmationNotice}
	case toolGetAccessRequestStatus:
		var arguments struct {
			RequestID *string `json:"requestId"`
		}
		if err = decodeToolArguments(call.Arguments, &arguments); err != nil || arguments.RequestID == nil || len(*arguments.RequestID) > maxRequestIDBytes {
			return "", ErrArguments
		}
		var requests []core.AccessRequest
		requests, err = tools.ListAccessRequests(ctx, user)
		state.nonKnowledge = true
		if *arguments.RequestID == "" {
			value = requests
		} else {
			value = map[string]any{"error": requestUnavailableMessage}
			for _, request := range requests {
				if request.ID == *arguments.RequestID {
					value = request
					break
				}
			}
		}
	default:
		return "", ErrArguments
	}
	state.used = true
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// Do not pass internal database/provider errors or identifiers to the model.
		return failedToolResultJSON, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf(toolResultErrorFormat, ErrUnavailable)
	}
	return string(data), nil
}

func buildHistoryMessages(history []core.Message) []message {
	if len(history) > maxHistoryMessages {
		history = history[len(history)-maxHistoryMessages:]
	}
	result := make([]message, 0, len(history))
	for _, item := range history {
		if item.Role != roleUser && item.Role != roleAssistant {
			continue
		}
		text := item.Text
		if len(text) > maxHistoryTextBytes {
			text = string([]rune(text)[:min(maxHistoryTextRunes, len([]rune(text)))])
		}
		result = append(result, message{Role: item.Role, Content: text})
	}
	return result
}
