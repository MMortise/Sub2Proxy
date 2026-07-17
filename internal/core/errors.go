package core

import "net/http"

// Error is a core error carrying an HTTP status code, so the API layer can map
// domain failures to 400/404/409 without string matching.
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// HTTPStatus returns the status code for err: a *core.Error's Code, or 500.
func HTTPStatus(err error) int {
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return http.StatusInternalServerError
}

func badRequest(msg string) *Error { return &Error{Code: http.StatusBadRequest, Msg: msg} }
func notFound(msg string) *Error   { return &Error{Code: http.StatusNotFound, Msg: msg} }
func conflict(msg string) *Error   { return &Error{Code: http.StatusConflict, Msg: msg} }
