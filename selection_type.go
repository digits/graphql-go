package graphql

import (
	"context"

	"github.com/graph-gophers/graphql-go/internal/exec/selections"
)

// SelectedFieldNamesForType returns the selected field paths beneath path for a
// concrete GraphQL object type, before a value of that type has been resolved.
// Path is relative to the current resolver and uses schema names, not aliases.
// For example, ("content", "StatementBlock") excludes fields selected only on
// other members of the content union. Nested field paths are flattened as in
// SelectedFieldNames. Named/inline fragments and @skip/@include are evaluated
// by the execution engine. __typename alone does not opt into any data fields.
// An absent path, incompatible type, or disabled selections returns no fields.
func SelectedFieldNamesForType(ctx context.Context, path, typeName string) []string {
	lazy := selections.FromContext(ctx)
	if lazy == nil {
		return []string{}
	}
	return lazy.NamesForType(path, typeName)
}
