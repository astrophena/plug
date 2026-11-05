// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package plughttp serves Plug methods and schema documentation over HTTP.
package plughttp

import (
	"bytes"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plugimpl"
)

// Options configures the handler returned by New.
type Options struct {
	// Authenticate derives a principal from each request. If nil, the
	// request's context supplies the principal.
	Authenticate func(*http.Request) (plug.Principal, error)

	// MaxBodyBytes limits JSON request bodies. Zero uses the server's
	// MaxFrameSize. It must not be negative.
	MaxBodyBytes int64
}

type statusResponse interface {
	HTTPStatus() int
}

// Binding maps a Plug method to an HTTP method and path.
type Binding = plug.HTTPBindingInfo

// Route serves an HTTP binding. Create routes with NewRoute.
type Route struct {
	server  *plug.Server
	binding Binding
	method  plug.MethodInfo
	handler func(http.ResponseWriter, *http.Request, int64)
}

// NewRoute returns a route for method and binding.
func NewRoute[Request, Response any](
	server *plug.Server,
	method plugimpl.Method[Request, Response],
	binding Binding,
) Route {
	info := method.Info()
	return Route{
		server:  server,
		binding: binding,
		method:  info,
		handler: func(w http.ResponseWriter, r *http.Request, maxBody int64) {
			var value Request
			if err := decodeRequest(r, method.Schema(), info, binding, &value, maxBody); err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", err, nil)
				return
			}

			response, err := plug.Invoke(r.Context(), server, method, value)
			if err != nil {
				writeMethodError(w, method, err)
				return
			}
			if _, ok := any(response).(plugimpl.Void); ok {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			encoded, err := json.Marshal(response)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", errors.New("encode response failed"), nil)
				return
			}
			status, err := responseStatus(response)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", err, nil)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if status != http.StatusOK {
				w.WriteHeader(status)
			}
			w.Write(append(encoded, '\n'))
		},
	}
}

func responseStatus(response any) (int, error) {
	status := http.StatusOK
	if value, ok := response.(statusResponse); ok {
		status = value.HTTPStatus()
	}
	if status < 200 || status > 599 {
		return 0, fmt.Errorf("invalid projected response status %d", status)
	}
	return status, nil
}

// New returns an HTTP handler for routes, which must belong to server and match
// its schema. Invalid or conflicting HTTP patterns return an error.
//
// Non-void responses with an HTTPStatus() int method select the response status.
// The status must be between 200 and 599. Other responses use 200; void uses 204.
func New(server *plug.Server, routes []Route, opts Options) (http.Handler, error) {
	if opts.MaxBodyBytes < 0 {
		return nil, errors.New("plughttp: MaxBodyBytes cannot be negative")
	}

	if err := server.Seal(); err != nil {
		return nil, err
	}
	if opts.MaxBodyBytes == 0 {
		opts.MaxBodyBytes = int64(server.Limits().MaxFrameSize)
	}

	var (
		schema = server.Schema()
		mux    = http.NewServeMux()
	)
	for _, route := range routes {
		if route.handler == nil {
			return nil, fmt.Errorf("plughttp: route %s %s has no handler", route.binding.Verb, route.binding.Path)
		}
		if route.server != server {
			return nil, fmt.Errorf("plughttp: route %s %s belongs to a different server", route.binding.Verb, route.binding.Path)
		}

		known, ok := plugimpl.LookupMethod(schema, route.method.ID)
		if !ok || known.Name != route.method.Name {
			return nil, fmt.Errorf("plughttp: route method %q is absent from schema", route.method.Name)
		}

		if !slices.Contains(known.HTTP, route.binding) {
			return nil, fmt.Errorf("plughttp: route %s %s is not declared by method %s", route.binding.Verb, route.binding.Path, known.Name)
		}

		pattern := route.binding.Verb + " " + route.binding.Path
		if err := handleRoute(mux, pattern, func(w http.ResponseWriter, r *http.Request) {
			route.handler(w, r, opts.MaxBodyBytes)
		}); err != nil {
			return nil, err
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opts.Authenticate != nil {
			principal, err := opts.Authenticate(r)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthenticated", err, nil)
				return
			}
			r = r.WithContext(plug.WithPrincipal(r.Context(), principal))
		}

		mux.ServeHTTP(w, r)
	}), nil
}

func handleRoute(mux *http.ServeMux, pattern string, handler http.HandlerFunc) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("plughttp: invalid route %s: %v", pattern, v)
		}
	}()
	mux.HandleFunc(pattern, handler)
	return nil
}

func decodeRequest(r *http.Request, schema *plug.Schema, method plug.MethodInfo, binding Binding, target any, maxBody int64) error {
	values := make(map[string]jsontext.Value, len(method.Fields))

	if binding.Verb != http.MethodGet && binding.Verb != http.MethodDelete && r.Body != nil {
		var reader io.Reader = r.Body
		if maxBody < math.MaxInt64 {
			reader = io.LimitReader(r.Body, maxBody+1)
		} else {
			reader = io.LimitReader(r.Body, maxBody)
		}

		body, err := io.ReadAll(reader)
		if err != nil {
			return fmt.Errorf("read JSON body: %w", err)
		}

		if int64(len(body)) > maxBody {
			return fmt.Errorf("JSON body exceeds %d bytes", maxBody)
		}

		if len(bytes.TrimSpace(body)) != 0 {
			if err := json.Unmarshal(body, &values); err != nil {
				return fmt.Errorf("decode JSON body: %w", err)
			}
			if values == nil {
				return errors.New("JSON body must be an object")
			}
		}
	}

	query := r.URL.Query()
	known := make(map[string]bool, len(method.Fields))
	for _, field := range method.Fields {
		known[field.Name] = true
	}
	for name := range query {
		if !known[name] {
			return fmt.Errorf("unknown query parameter %q", name)
		}
	}

	for _, field := range method.Fields {
		text, present := query[field.Name]
		if strings.Contains(binding.Path, "{"+field.Name+"}") || strings.Contains(binding.Path, "{"+field.Name+"...}") {
			text = []string{r.PathValue(field.Name)}
			present = true
		}
		if !present {
			continue
		}
		raw, err := textJSON(schema, field.Type, text)
		if err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}
		values[field.Name] = raw
	}

	if err := validateJSONFields(schema, method.Fields, values, false); err != nil {
		return err
	}

	encoded, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("assemble request: %w", err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		return fmt.Errorf("decode request fields: %w", err)
	}

	return nil
}

func validateJSONValue(schema *plug.Schema, expr plug.TypeExpr, raw jsontext.Value) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if expr.Optional {
			return nil
		}
		return errors.New("value is required")
	}

	if expr.Kind == plug.TypeExprList {
		var values []jsontext.Value
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}

		if expr.Element != nil {
			for i, value := range values {
				if err := validateJSONValue(schema, *expr.Element, value); err != nil {
					return fmt.Errorf("item %d: %w", i, err)
				}
			}
		}

		return nil
	}

	var typ *plug.TypeInfo
	for i := range schema.Types {
		if schema.Types[i].Name == expr.Name {
			typ = &schema.Types[i]
			break
		}
	}
	if typ == nil {
		return nil
	}
	if typ.Kind == plug.TypeNewtype {
		return validateJSONValue(schema, plug.TypeExpr{
			Kind: plug.TypeExprPrimitive,
			Name: typ.Underlying,
		}, raw)
	}

	var values map[string]jsontext.Value
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	if typ.Kind == plug.TypeSum {
		var constructorName string
		if tag, ok := values["type"]; ok {
			if err := json.Unmarshal(tag, &constructorName); err != nil {
				return fmt.Errorf("decode %s type: %w", typ.Name, err)
			}
		}
		if constructorName == "" {
			return fmt.Errorf("%s constructor type is required", typ.Name)
		}
		for _, constructor := range typ.Constructors {
			if constructor.Name == constructorName {
				return validateJSONFields(schema, constructor.Fields, values, true)
			}
		}
		return fmt.Errorf("unknown %s constructor %q", typ.Name, constructorName)
	}

	var fields []plug.FieldInfo
	if len(typ.Constructors) == 1 {
		fields = typ.Constructors[0].Fields
	}
	return validateJSONFields(schema, fields, values, false)
}

func validateJSONFields(schema *plug.Schema, fields []plug.FieldInfo, values map[string]jsontext.Value, sum bool) error {
	known := make(map[string]bool, len(fields)+1)
	if sum {
		known["type"] = true
	}
	for _, field := range fields {
		known[field.Name] = true
		value, present := values[field.Name]
		if !present {
			if field.Type.Optional {
				continue
			}
			return fmt.Errorf("field %s is required", field.Name)
		}
		if err := validateJSONValue(schema, field.Type, value); err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}
	}

	for name := range values {
		if !known[name] {
			return fmt.Errorf("unknown field %q", name)
		}
	}

	return nil
}

func textJSON(schema *plug.Schema, expr plug.TypeExpr, values []string) (jsontext.Value, error) {
	if expr.Kind == plug.TypeExprList {
		elem := plug.TypeExpr{
			Kind: plug.TypeExprPrimitive,
			Name: "string",
		}
		if expr.Element != nil {
			elem = *expr.Element
		}
		items := make([]jsontext.Value, 0, len(values))
		for _, value := range values {
			item, err := scalarJSON(schema, elem, value)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return json.Marshal(items)
	}

	if len(values) > 1 {
		return nil, fmt.Errorf("duplicate parameter")
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("expected one value")
	}
	return scalarJSON(schema, expr, values[0])
}

func scalarJSON(schema *plug.Schema, expr plug.TypeExpr, value string) (jsontext.Value, error) {
	name := expr.Name
	for _, typ := range schema.Types {
		if typ.Name == name && typ.Kind == plug.TypeNewtype {
			name = typ.Underlying
			break
		}
	}

	switch name {
	case "bool":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("parse bool %q: %w", value, err)
		}
		return json.Marshal(b)
	case "i32", "i64":
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse integer %q: %w", value, err)
		}
		return json.Marshal(v)
	case "u32", "u64":
		v, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse integer %q: %w", value, err)
		}
		return json.Marshal(v)
	case "f64":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("parse float %q: %w", value, err)
		}
		return json.Marshal(v)
	case "bytes":
		data, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("parse bytes %q: %w", value, err)
		}
		return json.Marshal(data)
	default:
		return json.Marshal(value)
	}
}

func writeMethodError[Request, Response any](
	w http.ResponseWriter,
	method plugimpl.Method[Request, Response],
	err error,
) {
	if info, ok := method.ErrorInfo(err); ok {
		status := info.HTTPStatus
		if status == 0 {
			status = http.StatusBadRequest
		}
		writeError(w, status, info.Name, err, err)
		return
	}

	if remote, ok := errors.AsType[*plug.RemoteError](err); ok {
		status := http.StatusInternalServerError
		code := "internal"
		switch remote.Code {
		case plug.ErrorBadRequest:
			status, code = http.StatusBadRequest, "bad_request"
		case plug.ErrorUnauthenticated:
			status, code = http.StatusUnauthorized, "unauthenticated"
		case plug.ErrorPermissionDenied:
			status, code = http.StatusForbidden, "permission_denied"
		case plug.ErrorUnimplemented:
			status, code = http.StatusNotImplemented, "unimplemented"
		}
		writeError(w, status, code, remote, nil)
		return
	}

	writeError(w, http.StatusInternalServerError, "internal", err, nil)
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitzero"`
}

func writeError(w http.ResponseWriter, status int, code string, err error, details any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	msg := err.Error()
	if status == http.StatusInternalServerError {
		msg = "internal server error"
	}
	json.MarshalEncode(jsontext.NewEncoder(w), errorEnvelope{
		Code:    code,
		Message: msg,
		Details: details,
	})
}
