package oaschecker

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type requestSender func(instructions requestInstructions) *http.Response

func TestMiddleware_ServeHTTP(t *testing.T) {
	router := loadPetStoreRouter(t)

	t.Run("valid GET request", func(t *testing.T) {
		responseBody := `[{"id": 123, "name": "Buddy"}]`
		sendRequest, middleware := setUp(t, router, func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(responseBody))
		})

		res := sendRequest(requestInstructions{
			Method: "GET",
			URI:    "http://petstore.swagger.io/v1/pets",
		})
		assert.Equal(t, 200, res.StatusCode)

		receivedResponse, err := ioutil.ReadAll(res.Body)
		require.NoError(t, err)
		assert.Equal(t, responseBody, string(receivedResponse), "response body should arrive unchanged")

		assert.Empty(t, middleware.issues, "no issues should have been recorded")
	})

	t.Run("valid POST request", func(t *testing.T) {
		var receivedRequestBody string
		sendRequest, middleware := setUp(t, router, func(rw http.ResponseWriter, r *http.Request) {
			body, err := ioutil.ReadAll(r.Body)
			require.NoError(t, err)
			receivedRequestBody = string(body)
			rw.WriteHeader(http.StatusOK)
		})

		requestBody := `{"id": 123, "name": "Buddy"}`
		res := sendRequest(requestInstructions{
			Method:  "POST",
			URI:     "http://petstore.swagger.io/v1/pets",
			Body:    []byte(requestBody),
			Headers: map[string]string{"Content-Type": "application/json"},
		})
		assert.Equal(t, 200, res.StatusCode)

		assert.Equal(t, requestBody, receivedRequestBody, "request body should arrive unchanged")

		assert.Empty(t, middleware.issues, "no issues should have been recorded")
	})

	t.Run("should raise issue of unknown route", func(t *testing.T) {
		sendRequest, middleware := setUp(t, router, func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusOK)
		})

		res := sendRequest(requestInstructions{
			Method: "GET",
			URI:    "http://petstore.swagger.io/some-undocumented-path",
		})
		assert.Equal(t, 200, res.StatusCode)

		assert.Equal(t, []validationIssue{
			{
				Method:      "GET",
				URI:         "http://petstore.swagger.io/some-undocumented-path",
				Description: "Route not found in specification: Does not match any server",
			},
		}, middleware.issues)
	})

	t.Run("should raise issue with response format", func(t *testing.T) {
		responseBody := `[{"id": 123, "name": "Buddy"}]`
		sendRequest, middleware := setUp(t, router, func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(responseBody))
		})

		res := sendRequest(requestInstructions{
			Method: "GET",
			URI:    "http://petstore.swagger.io/v1/pets",
		})
		assert.Equal(t, 200, res.StatusCode)

		receivedResponse, err := ioutil.ReadAll(res.Body)
		require.NoError(t, err)
		assert.Equal(t, responseBody, string(receivedResponse), "response body should arrive unchanged")

		assert.Equal(t, []validationIssue{
			{
				Method:      "GET",
				URI:         "http://petstore.swagger.io/v1/pets",
				Description: `Invalid response: input header 'Content-Type' has unexpected value: ""`,
			},
		}, middleware.issues)
	})

	t.Run("should raise issue with request format", func(t *testing.T) {
		var receivedRequestBody string
		sendRequest, middleware := setUp(t, router, func(rw http.ResponseWriter, r *http.Request) {
			body, err := ioutil.ReadAll(r.Body)
			require.NoError(t, err)
			receivedRequestBody = string(body)
			rw.WriteHeader(http.StatusOK)
		})

		requestBody := `{"id": 123, "name": "Buddy"}`
		res := sendRequest(requestInstructions{
			Method: "POST",
			URI:    "http://petstore.swagger.io/v1/pets",
			Body:   []byte(requestBody),
		})
		assert.Equal(t, 200, res.StatusCode)

		assert.Equal(t, requestBody, receivedRequestBody, "request body should arrive unchanged")

		assert.Equal(t, []validationIssue{
			{
				Method:      "POST",
				URI:         "http://petstore.swagger.io/v1/pets",
				Description: `Invalid request: Request body has an error: header 'Content-Type' has unexpected value: ""`,
			},
		}, middleware.issues)
	})

}

type requestInstructions struct {
	Method  string
	URI     string
	Body    []byte
	Headers map[string]string
}

func setUp(t *testing.T, router *openapi3filter.Router, handler http.HandlerFunc) (requestSender, *Middleware) {
	t.Helper()

	middleware := &Middleware{router: router, next: handler}

	server := httptest.NewServer(middleware)
	t.Cleanup(server.Close)

	proxyURI, _ := url.Parse(server.URL)
	proxy := http.ProxyURL(proxyURI)
	server.Client().Transport = &http.Transport{Proxy: proxy}

	return func(instructions requestInstructions) *http.Response {
		t.Helper()

		var body io.Reader
		if instructions.Body != nil {
			body = bytes.NewBuffer(instructions.Body)
		}

		req, err := http.NewRequest(instructions.Method, instructions.URI, body)
		require.NoError(t, err)

		for k, v := range instructions.Headers {
			req.Header.Set(k, v)
		}

		res, err := server.Client().Do(req)
		require.NoError(t, err)
		return res
	}, middleware
}

func loadPetStoreRouter(t *testing.T) *openapi3filter.Router {
	t.Helper()

	swagger, err := openapi3.NewSwaggerLoader().LoadSwaggerFromData([]byte(petStore))
	require.NoError(t, err)

	return openapi3filter.NewRouter().WithSwagger(swagger)
}

const petStore = `openapi: "3.0.0"
info:
  version: 1.0.0
  title: Swagger Petstore
  license:
    name: MIT
servers:
  - url: http://petstore.swagger.io/v1
paths:
  /pets:
    get:
      summary: List all pets
      operationId: listPets
      tags:
        - pets
      parameters:
        - name: limit
          in: query
          description: How many items to return at one time (max 100)
          required: false
          schema:
            type: integer
            format: int32
      responses:
        '200':
          description: A paged array of pets
          headers:
            x-next:
              description: A link to the next page of responses
              schema:
                type: string
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Pets"
        default:
          description: unexpected error
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
    post:
      summary: Create a pet
      operationId: createPets
      tags:
        - pets
      requestBody:
        description: Pet to add to the store
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/NewPet'
      responses:
        '201':
          description: Null response
        default:
          description: unexpected error
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
components:
  schemas:
    NewPet:
      type: object
      required:
        - name
      properties:
        name:
          type: string
        tag:
          type: string
    Pet:
      type: object
      required:
        - id
        - name
      properties:
        id:
          type: integer
          format: int64
        name:
          type: string
        tag:
          type: string
    Pets:
      type: array
      items:
        $ref: "#/components/schemas/Pet"
    Error:
      type: object
      required:
        - code
        - message
      properties:
        code:
          type: integer
          format: int32
        message:
          type: string`
