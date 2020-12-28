package oaschecker

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3filter"
)

var _ http.Handler = &Middleware{}

type Middleware struct {
	router *openapi3filter.Router
	next   http.Handler
	mu     sync.Mutex
	issues []validationIssue
}

func (c *Middleware) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, pathParams, err := c.router.FindRoute(r.Method, r.URL)
	if err != nil {
		c.addIssue(r.Method, r.URL, fmt.Sprintf("Route not found in specification: %v", err))
		c.next.ServeHTTP(rw, r)
		return
	}

	reqValInput := &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: pathParams,
		Route:      route,
	}
	if err := openapi3filter.ValidateRequest(r.Context(), reqValInput); err != nil {
		c.addIssue(r.Method, r.URL, fmt.Sprintf("Invalid request: %v", err))
	}

	recorder := httptest.NewRecorder()
	c.next.ServeHTTP(recorder, r)
	for k, v := range recorder.HeaderMap {
		rw.Header()[k] = v
	}
	rw.WriteHeader(recorder.Code)

	bytes.NewBuffer(recorder.Body.Bytes()).WriteTo(rw)

	resValInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: reqValInput,
		Status:                 recorder.Code,
		Header:                 recorder.HeaderMap,
	}

	bodyBytes := recorder.Body.Bytes()
	if len(bodyBytes) > 0 {
		resValInput.SetBodyBytes(recorder.Body.Bytes())

		if err := openapi3filter.ValidateResponse(r.Context(), resValInput); err != nil {
			c.addIssue(r.Method, r.URL, fmt.Sprintf("Invalid response: %v", err))
		}
	}
}

func (c *Middleware) Validate() error {
	if len(c.issues) == 0 {
		return nil
	}

	descriptions := make([]string, len(c.issues))
	for i, issue := range c.issues {
		descriptions[i] = fmt.Sprintf("%v %v: %v", issue.Method, issue.URI, issue.Description)
	}

	return fmt.Errorf("Errors were found validating the API specification:\n%v",
		strings.Join(descriptions, "\n---\n"))
}

func (c *Middleware) addIssue(method string, url *url.URL, description string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.issues = append(c.issues, validationIssue{
		Method:      method,
		URI:         url.String(),
		Description: description,
	})
}

type validationIssue struct {
	Method      string
	URI         string
	Description string
}
