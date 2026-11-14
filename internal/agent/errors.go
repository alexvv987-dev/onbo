package agent

import "errors"

const (
	errArgumentsMessage   = "модель предложила недопустимый вызов инструмента"
	errLimitMessage       = "достигнут предел обращений к инструментам"
	errUnavailableMessage = "GigaChat временно недоступен"
)

var (
	ErrUnavailable = errors.New(errUnavailableMessage)
	ErrArguments   = errors.New(errArgumentsMessage)
	ErrLimit       = errors.New(errLimitMessage)
)
