package selections

import (
	"strings"

	"github.com/graph-gophers/graphql-go/internal/exec/resolvable"
	"github.com/graph-gophers/graphql-go/internal/exec/selected"
)

// NamesForType inspects the engine's directive-filtered selections, retaining
// the concrete type condition at the requested object path.
func (l *Lazy) NamesForType(path, typeName string) []string {
	names := []string{}
	if path == "" || typeName == "" {
		return names
	}
	collectTypePaths(&names, make(map[string]struct{}), l.raw, strings.Split(path, "."), typeName)
	return names
}

func collectTypePaths(dst *[]string, seen map[string]struct{}, sels []selected.Selection, path []string, typeName string) {
	for _, sel := range sels {
		switch s := sel.(type) {
		case *selected.TypeAssertion:
			collectTypePaths(dst, seen, s.Sels, path, typeName)
		case *selected.SchemaField:
			if s.Name != path[0] {
				continue
			}
			if len(path) > 1 {
				collectTypePaths(dst, seen, s.Sels, path[1:], typeName)
				continue
			}
			value := s.ValueExec
			for {
				list, ok := value.(*resolvable.List)
				if !ok {
					break
				}
				value = list.Elem
			}
			object, ok := value.(*resolvable.Object)
			if !ok {
				continue
			}
			if object.Name != typeName && object.TypeAssertions[typeName] == nil {
				continue
			}
			collectConcretePaths(dst, seen, s.Sels, typeName)
		}
	}
}

func collectConcretePaths(dst *[]string, seen map[string]struct{}, sels []selected.Selection, typeName string) {
	for _, sel := range sels {
		if assertion, ok := sel.(*selected.TypeAssertion); ok {
			if object, ok := assertion.TypeExec.(*resolvable.Object); ok && object.Name == typeName {
				collectConcretePaths(dst, seen, assertion.Sels, typeName)
			}
			continue
		}
		collectNestedPaths(dst, seen, "", []selected.Selection{sel})
	}
}
