// Package apperr define erros de aplicação tipados e sua serialização
// no formato application/problem+json (RFC 7807).
package apperr

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Kind classifica o erro e determina o status HTTP.
type Kind int

// Tipos de erro suportados, do mais genérico ao mais específico.
const (
	KindInternal Kind = iota
	KindValidation
	KindUnauthorized
	KindForbidden
	KindNotFound
	KindConflict
	KindTooManyRequests
	KindUnavailable
)

func (k Kind) status() int {
	switch k {
	case KindValidation:
		return http.StatusBadRequest
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindForbidden:
		return http.StatusForbidden
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindTooManyRequests:
		return http.StatusTooManyRequests
	case KindUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// Error é um erro de aplicação com contexto suficiente para virar uma resposta HTTP.
type Error struct {
	Kind    Kind
	Title   string
	Detail  string
	wrapped error
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Title + ": " + e.Detail
	}
	return e.Title
}

func (e *Error) Unwrap() error { return e.wrapped }

// New cria um erro de aplicação.
func New(kind Kind, title, detail string) *Error {
	return &Error{Kind: kind, Title: title, Detail: detail}
}

// Wrap embrulha um erro existente preservando a cadeia para logs.
func Wrap(kind Kind, title string, err error) *Error {
	return &Error{Kind: kind, Title: title, Detail: err.Error(), wrapped: err}
}

// Is permite comparar pelo Kind via errors.Is.
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return e.Kind == t.Kind
}

// Problem é a representação RFC 7807.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// Write serializa o erro como application/problem+json. Qualquer erro não-*Error
// vira um 500 genérico (sem vazar detalhes internos ao cliente).
func Write(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *Error
	if !errors.As(err, &appErr) {
		appErr = Wrap(KindInternal, "Erro interno", err)
	}
	status := appErr.Kind.status()

	detail := appErr.Detail
	if appErr.Kind == KindInternal {
		detail = "" // não expor detalhes internos
	}

	p := Problem{
		Type:     "about:blank",
		Title:    appErr.Title,
		Status:   status,
		Detail:   detail,
		Instance: r.URL.Path,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}
