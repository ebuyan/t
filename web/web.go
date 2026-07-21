// Package web хранит статику веб-страницы: HTML-шаблоны, встроенные в бинарник.
// HTTP-сервер, который их отдаёт, живёт в internal/server.
package web

import (
	_ "embed"
	"html/template"
)

//go:embed page.html
var pageHTML string

// Page — страница доходности. Исполняется с portfolio.YieldView.
var Page = template.Must(template.New("page").Parse(pageHTML))

//go:embed error.html
var errorHTML string

// Error — страница-заглушка «данные ещё не готовы». Исполняется со
// строкой-сообщением об ошибке.
var Error = template.Must(template.New("error").Parse(errorHTML))
