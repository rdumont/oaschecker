package oaschecker

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3filter"
)

type Options struct {
	File string
}

func New(opt Options) (*Checker, error) {
	router := openapi3filter.NewRouter()
	if err := router.AddSwaggerFromFile(opt.File); err != nil {
		return nil, err
	}

	return &Checker{router: router}, nil
}

type Checker struct {
	router *openapi3filter.Router
}

func (c *Checker) Middleware(next http.Handler) *Middleware {
	return &Middleware{router: c.router, next: next}
}
