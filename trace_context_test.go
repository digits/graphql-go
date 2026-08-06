package graphql_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/graph-gophers/graphql-go"
	gqlerrors "github.com/graph-gophers/graphql-go/errors"
	"github.com/graph-gophers/graphql-go/introspection"
)

type traceContextKey struct{}

type traceContextTracer struct {
	mu       sync.Mutex
	parents  map[string]string
	finished map[string][]*gqlerrors.QueryError
}

func newTraceContextTracer() *traceContextTracer {
	return &traceContextTracer{
		parents:  make(map[string]string),
		finished: make(map[string][]*gqlerrors.QueryError),
	}
}

func (t *traceContextTracer) TraceQuery(ctx context.Context, _ string, _ string, _ map[string]interface{}, _ map[string]*introspection.Type) (context.Context, func([]*gqlerrors.QueryError)) {
	return ctx, func([]*gqlerrors.QueryError) {}
}

func (t *traceContextTracer) TraceField(ctx context.Context, _ string, typeName string, fieldName string, _ bool, _ map[string]interface{}) (context.Context, func(*gqlerrors.QueryError)) {
	field := typeName + "." + fieldName
	parent, _ := ctx.Value(traceContextKey{}).(string)

	t.mu.Lock()
	t.parents[field] = parent
	t.mu.Unlock()

	traceCtx := context.WithValue(ctx, traceContextKey{}, field)
	return traceCtx, func(err *gqlerrors.QueryError) {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.finished[field] = append(t.finished[field], err)
	}
}

type traceContextResolver struct {
	mu         sync.Mutex
	contexts   map[string]string
	selections map[string][]string
	err        error
}

func newTraceContextResolver(err error) *traceContextResolver {
	return &traceContextResolver{
		contexts:   make(map[string]string),
		selections: make(map[string][]string),
		err:        err,
	}
}

func (r *traceContextResolver) record(field string, ctx context.Context) {
	value, _ := ctx.Value(traceContextKey{}).(string)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contexts[field] = value
	r.selections[field] = graphql.SelectedFieldNames(ctx)
}

func (r *traceContextResolver) Object(ctx context.Context) *traceContextObjectResolver {
	r.record("Query.object", ctx)
	return &traceContextObjectResolver{root: r}
}

func (r *traceContextResolver) Leaf(ctx context.Context) string {
	r.record("Query.leaf", ctx)
	return "leaf"
}

func (r *traceContextResolver) Failing(ctx context.Context) (*string, error) {
	r.record("Query.failing", ctx)
	return nil, r.err
}

type traceContextObjectResolver struct {
	root *traceContextResolver
}

func (r *traceContextObjectResolver) Name(ctx context.Context) string {
	r.root.record("Object.name", ctx)
	return "object"
}

func TestTraceFieldContextReachesResolvers(t *testing.T) {
	t.Parallel()

	resolverErr := errors.New("resolver error")
	resolver := newTraceContextResolver(resolverErr)
	fieldTracer := newTraceContextTracer()
	schema := graphql.MustParseSchema(`
		type Query {
			object: Object!
			leaf: String!
			failing: String
		}

		type Object {
			name: String!
		}
	`, resolver, graphql.Tracer(fieldTracer))

	ctx := context.WithValue(context.Background(), traceContextKey{}, "request")
	response := schema.Exec(ctx, `{ object { name } leaf failing }`, "", nil)

	if got, want := string(response.Data), `{"object":{"name":"object"},"leaf":"leaf","failing":null}`; got != want {
		t.Errorf("response data: got %s, want %s", got, want)
	}

	wantResponseError := &gqlerrors.QueryError{
		Err:           resolverErr,
		Message:       "resolver error",
		Path:          []interface{}{"failing"},
		ResolverError: resolverErr,
	}
	if got, want := len(response.Errors), 1; got != want {
		t.Fatalf("response error count: got %d, want %d", got, want)
	}
	if !reflect.DeepEqual(response.Errors[0], wantResponseError) {
		t.Errorf("response error: got %#v, want %#v", *response.Errors[0], *wantResponseError)
	}

	wantContexts := map[string]string{
		"Query.object":  "Query.object",
		"Object.name":   "Object.name",
		"Query.leaf":    "Query.leaf",
		"Query.failing": "Query.failing",
	}
	if !reflect.DeepEqual(resolver.contexts, wantContexts) {
		t.Errorf("resolver contexts: got %#v, want %#v", resolver.contexts, wantContexts)
	}
	wantSelections := map[string][]string{
		"Query.object":  {"name"},
		"Object.name":   {},
		"Query.leaf":    {},
		"Query.failing": {},
	}
	if !reflect.DeepEqual(resolver.selections, wantSelections) {
		t.Errorf("resolver selections: got %#v, want %#v", resolver.selections, wantSelections)
	}

	wantParents := map[string]string{
		"Query.object":  "request",
		"Object.name":   "Query.object",
		"Query.leaf":    "request",
		"Query.failing": "request",
	}
	if !reflect.DeepEqual(fieldTracer.parents, wantParents) {
		t.Errorf("trace parents: got %#v, want %#v", fieldTracer.parents, wantParents)
	}

	wantFinished := map[string][]*gqlerrors.QueryError{
		"Query.object": {nil},
		"Object.name":  {nil},
		"Query.leaf":   {nil},
		"Query.failing": {{
			Err:           resolverErr,
			Message:       "resolver error",
			Path:          []interface{}{"failing"},
			ResolverError: resolverErr,
		}},
	}
	if !reflect.DeepEqual(fieldTracer.finished, wantFinished) {
		t.Errorf("finished traces: got %#v, want %#v", fieldTracer.finished, wantFinished)
	}
}
