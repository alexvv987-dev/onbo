package backend

import "errors"

const (
	errForbiddenMessage   = "Операция недоступна"
	errInvalidMessage     = "Проверьте данные запроса"
	errNotFoundMessage    = "Объект не найден"
	errUnavailableMessage = "Сервис временно недоступен. Результат можно проверить повторно"
)

var (
	ErrNotFound    = errors.New(errNotFoundMessage)
	ErrInvalid     = errors.New(errInvalidMessage)
	ErrForbidden   = errors.New(errForbiddenMessage)
	ErrUnavailable = errors.New(errUnavailableMessage)
)
