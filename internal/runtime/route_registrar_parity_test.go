package runtime

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A connector registers its flows by being found as a RouteRegistrar. The
// interface is written with an unnamed function type, and Go asks for an
// identical type, not a compatible one — so a connector that spells the same
// parameter as its own `HandlerFunc` does not implement the interface.
//
// Nothing catches that. It compiles, the method looks right beside its
// neighbours, the connector starts and reports itself listening, and the
// banner prints the flows. The runtime asks whether it is a RouteRegistrar,
// is told no, and moves on without a word. Two connectors were in that state:
// a TCP server answered "unknown message type" to every message it was sent,
// and a flow reading from a remote GraphQL subscription received nothing.
//
// The check is on the source rather than on the types because the types are
// exactly what does not complain. It reads every RegisterRoute declaration
// under internal/connector and requires the handler parameter to be written
// out in full.
func TestEveryRegisterRouteCanActuallyBeFound(t *testing.T) {
	const want = "func(ctx context.Context, input map[string]interface{}) (interface{}, error)"

	declarations := registerRouteDeclarations(t)
	if len(declarations) < 10 {
		t.Fatalf("found %d RegisterRoute declarations, too few to be the whole set", len(declarations))
	}

	for _, found := range declarations {
		if found.handlerType != want {
			t.Errorf("%s: %s takes a handler of type %s, so it does not satisfy runtime.RouteRegistrar "+
				"and the runtime will never register a flow on it. Write the parameter as %s.",
				found.position, found.receiver, found.handlerType, want)
		}
	}
}

type registerRoute struct {
	position    string
	receiver    string
	handlerType string
}

// registerRouteDeclarations returns every RegisterRoute method declared under
// internal/connector, with the source text of its handler parameter.
func registerRouteDeclarations(t *testing.T) []registerRoute {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", "connector"))
	if err != nil {
		t.Fatalf("locating the connector packages: %v", err)
	}

	var found []registerRoute
	fset := token.NewFileSet()

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "RegisterRoute" || fn.Recv == nil {
				continue
			}
			// The handler is the second parameter; the first is the operation.
			params := fn.Type.Params.List
			if len(params) < 2 {
				continue
			}
			var handler bytes.Buffer
			if err := printer.Fprint(&handler, fset, params[len(params)-1].Type); err != nil {
				return err
			}
			found = append(found, registerRoute{
				position:    fset.Position(fn.Pos()).String(),
				receiver:    receiverName(fn),
				handlerType: handler.String(),
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the connector packages: %v", err)
	}

	return found
}

func receiverName(fn *ast.FuncDecl) string {
	if len(fn.Recv.List) == 0 {
		return "RegisterRoute"
	}
	switch receiver := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if ident, ok := receiver.X.(*ast.Ident); ok {
			return "*" + ident.Name + ".RegisterRoute"
		}
	case *ast.Ident:
		return receiver.Name + ".RegisterRoute"
	}
	return "RegisterRoute"
}
