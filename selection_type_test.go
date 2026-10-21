package graphql_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/graph-gophers/graphql-go"
)

const typedSelectionSchema = `
 schema { query: Query subscription: Subscription }
 type Query { event: Event! }
 type Subscription { event: Event! }
 type Event { content: [Block!]! }
 union Block = TransactionBlock | InvoiceBlock
 interface Named { title: String! }
 type TransactionBlock implements Named { id: ID! title: String! viewKey: ViewKey! }
 type InvoiceBlock implements Named { id: ID! title: String! viewKey: ViewKey! }
 type ViewKey { legalEntityId: ID! }
`

type typedSelectionRoot struct {
	check func(context.Context)
}
type typedSelectionQuery struct{ root *typedSelectionRoot }
type typedSelectionSubscription struct{ root *typedSelectionRoot }
type typedSelectionEvent struct{}
type typedSelectionBlock struct{}
type typedSelectionData struct{}
type typedSelectionViewKey struct{}

func (r *typedSelectionRoot) Query() *typedSelectionQuery { return &typedSelectionQuery{r} }
func (r *typedSelectionRoot) Subscription() *typedSelectionSubscription {
	return &typedSelectionSubscription{r}
}
func (r *typedSelectionQuery) Event(ctx context.Context) *typedSelectionEvent {
	r.root.check(ctx)
	return &typedSelectionEvent{}
}
func (r *typedSelectionSubscription) Event(ctx context.Context) <-chan *typedSelectionEvent {
	r.root.check(ctx)
	ch := make(chan *typedSelectionEvent, 1)
	ch <- &typedSelectionEvent{}
	close(ch)
	return ch
}
func (*typedSelectionEvent) Content() []*typedSelectionBlock { return []*typedSelectionBlock{{}} }
func (*typedSelectionBlock) ToTransactionBlock() (*typedSelectionData, bool) {
	return &typedSelectionData{}, true
}
func (*typedSelectionBlock) ToInvoiceBlock() (*typedSelectionData, bool) { return nil, false }
func (*typedSelectionData) ID() graphql.ID                               { return "id" }
func (*typedSelectionData) Title() string                                { return "title" }
func (*typedSelectionData) ViewKey() *typedSelectionViewKey              { return &typedSelectionViewKey{} }
func (*typedSelectionViewKey) LegalEntityID() graphql.ID                 { return "le" }

func TestSelectedFieldNamesForType(t *testing.T) {
	for _, operation := range []string{"query", "subscription"} {
		for _, tc := range []struct {
			name, variables, selection, fragments string
			values                                map[string]any
			transaction, invoice                  []string
		}{
			{name: "overlapping field names and aliases", selection: `content { ... on TransactionBlock { id tx: title viewKey { legalEntityId } } ... on InvoiceBlock { id } }`, transaction: []string{"id", "title", "viewKey", "viewKey.legalEntityId"}, invoice: []string{"id"}},
			{name: "named fragments and repeated aliased fields", selection: `first: content { ...Transactions } second: content { ... on TransactionBlock { title } }`, fragments: `fragment Transactions on Block { ... on TransactionBlock { title viewKey { legalEntityId } } }`, transaction: []string{"title", "viewKey", "viewKey.legalEntityId"}},
			{name: "typename is not payload", selection: `content { __typename ... on InvoiceBlock { __typename } }`},
			{name: "directives", variables: `($include: Boolean!, $skip: Boolean!)`, values: map[string]any{"include": false, "skip": true}, selection: `content { ...Transactions @include(if: $include) ... on InvoiceBlock @skip(if: $skip) { title } }`, fragments: `fragment Transactions on TransactionBlock { title }`},
			{name: "directive variable defaults", variables: `($include: Boolean = false, $skip: Boolean = true)`, selection: `content { ... on TransactionBlock @include(if: $include) { title } ... on InvoiceBlock { title @skip(if: $skip) } }`},
			{name: "skipped content", selection: `content @skip(if: true) { ... on TransactionBlock { title } }`},
			{name: "interface condition", selection: `content { ... on Named { title } ... on InvoiceBlock { id } }`, transaction: []string{"title"}, invoice: []string{"title", "id"}},
		} {
			for _, disabled := range []bool{false, true} {
				t.Run(operation+"/"+tc.name+map[bool]string{true: "/disabled"}[disabled], func(t *testing.T) {
					called := false
					root := &typedSelectionRoot{check: func(ctx context.Context) {
						called = true
						for typ, expected := range map[string][]string{"TransactionBlock": tc.transaction, "InvoiceBlock": tc.invoice, "Unknown": nil} {
							if disabled {
								expected = nil
							}
							actual := graphql.SelectedFieldNamesForType(ctx, "content", typ)
							if len(actual) != len(expected) || len(actual) > 0 && !reflect.DeepEqual(actual, expected) {
								t.Errorf("%s selections = %v, want %v", typ, actual, expected)
							}
							if len(actual) > 0 {
								actual[0] = "changed"
								if graphql.SelectedFieldNamesForType(ctx, "content", typ)[0] == "changed" {
									t.Error("caller mutated selection state")
								}
							}
						}
						if disabled && len(graphql.SelectedFieldNames(ctx)) > 0 {
							t.Error("selection opt-out ignored")
						}
						if !disabled && len(tc.transaction)+len(tc.invoice) > 0 && !graphql.HasSelectedField(ctx, "content") {
							t.Error("ordinary selections unavailable")
						}
						if len(graphql.SelectedFieldNamesForType(ctx, "missing", "TransactionBlock")) != 0 {
							t.Error("missing path matched")
						}
					}}
					var opts []graphql.SchemaOpt
					if disabled {
						opts = append(opts, graphql.DisableFieldSelections())
					}
					schema := graphql.MustParseSchema(typedSelectionSchema, root, opts...)
					query := operation + " Select" + tc.variables + " { event { " + tc.selection + " } } " + tc.fragments
					if operation == "query" {
						response := schema.Exec(context.Background(), query, "Select", tc.values)
						if len(response.Errors) > 0 {
							t.Fatal(response.Errors)
						}
					} else {
						responses, err := schema.Subscribe(context.Background(), query, "Select", tc.values)
						if err != nil {
							t.Fatal(err)
						}
						for response := range responses {
							if errs := response.(*graphql.Response).Errors; len(errs) > 0 {
								t.Fatal(errs)
							}
						}
					}
					if !called {
						t.Fatal("resolver not called")
					}
				})
			}
		}
	}
}
