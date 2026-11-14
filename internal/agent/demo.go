package agent

import (
	"context"
	"fmt"
	"strings"

	"onbo/internal/core"
)

const (
	demoDraftPreparedMessage = "Подготовил черновик заявки на доступ. Проверьте ресурс и основание, затем нажмите «Подтвердить и отправить». Заявка ещё не отправлена."
	demoInstructionPrefix    = "По тестовой инструкции «"
	demoInstructionSeparator = "»: "
	demoInstructionSuffix    = `

Это синтетические данные, не регламент Авито. Если нужен доступ, напишите: «Подготовь заявку на доступ к учебному репозиторию для первой задачи».`
	demoNoKnowledgeMessage     = "Подходящих доступных инструкций не найдено. Я не буду придумывать правила. Обратитесь к наставнику своей команды."
	nextStepPrefix             = "Следующий шаг: "
	nextStepSuffix             = ". Прогресс сохраняется в вашем плане справа."
	noAllowedResourcesMessage  = "Для вашего профиля нет доступных ресурсов для этой заявки. Обратитесь к наставнику своей команды."
	noSubmittedRequestsMessage = "У вас пока нет отправленных заявок. Черновик нужно подтвердить кнопкой."
	requestStatusFormat        = "Заявка %s: %s. %s"
)

// Demo is an explicitly selected deterministic substitute, never an error fallback.
type Demo struct{ tools core.Tools }

func NewDemo(tools core.Tools) *Demo { return &Demo{tools: tools} }

func (demo *Demo) GenerateReply(ctx context.Context, user core.User, _ []core.Message, text string) (core.ChatReply, error) {
	if !isValidString(text, maxChatMessageRunes) {
		return core.ChatReply{}, fmt.Errorf(invalidChatMessage)
	}
	if demo.tools == nil {
		return core.ChatReply{}, ErrUnavailable
	}
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	reply := core.ChatReply{Sources: []core.Source{}}
	if strings.Contains(lower, "статус") || strings.Contains(lower, "моя заявк") || strings.Contains(lower, "мои заявк") {
		requests, err := demo.tools.ListAccessRequests(ctx, user)
		if err != nil {
			return reply, err
		}
		if len(requests) == 0 {
			reply.Message = noSubmittedRequestsMessage
			return reply, nil
		}
		parts := []string{}
		for _, request := range requests {
			parts = append(parts, fmt.Sprintf(requestStatusFormat, request.ID, request.Status, request.Message))
		}
		reply.Message = strings.Join(parts, "\n")
		return reply, nil
	}
	if strings.Contains(lower, "план") || strings.Contains(lower, "следующ") || strings.Contains(lower, "что делать") || strings.Contains(lower, "дальше") {
		plan, err := demo.tools.GetOnboardingPlan(ctx, user)
		if err != nil {
			return reply, err
		}
		reply.Message = nextStepPrefix + plan.NextStep + nextStepSuffix
		return reply, nil
	}
	query := text
	switch {
	case strings.Contains(lower, "доступ"):
		query = "доступ"
	case strings.Contains(lower, "наставник") || strings.Contains(lower, "помощ") || strings.Contains(lower, "к кому") || strings.Contains(lower, "команд"):
		query = "наставник"
	case strings.Contains(lower, "перв") || strings.Contains(lower, "подготов") || strings.Contains(lower, "инструкц"):
		query = "первая задача"
	}
	sources, err := demo.tools.SearchKnowledge(ctx, user, query)
	if err != nil {
		return reply, err
	}
	if len(sources) > maxSources {
		sources = sources[:maxSources]
	}
	if sources != nil {
		reply.Sources = sources
	}
	if strings.Contains(lower, "доступ") && (strings.Contains(lower, "заяв") || strings.Contains(lower, "подготов") || strings.Contains(lower, "созда") || strings.Contains(lower, "запрос") || strings.Contains(lower, "нужен")) {
		resources, err := demo.tools.ListAccessResources(ctx, user)
		if err != nil {
			return reply, err
		}
		allowed := false
		for _, resource := range resources {
			if resource.ID == core.TrainingResourceID {
				allowed = true
				break
			}
		}
		if !allowed {
			reply.Message = noAllowedResourcesMessage
			return reply, nil
		}
		reason := []rune(text)
		if len(reason) > maxAccessReasonRunes {
			reason = reason[:maxAccessReasonRunes]
		}
		draft, err := demo.tools.CreateAccessDraft(ctx, user, core.TrainingResourceID, string(reason))
		if err != nil {
			return reply, err
		}
		reply.Draft = &draft
		reply.Message = demoDraftPreparedMessage
		return reply, nil
	}
	if len(reply.Sources) == 0 {
		reply.Message = demoNoKnowledgeMessage
		return reply, nil
	}
	reply.Message = demoInstructionPrefix + reply.Sources[0].Title + demoInstructionSeparator + reply.Sources[0].Excerpt + demoInstructionSuffix
	return reply, nil
}
