package runtime

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// Every place the language points at something by name.
//
// A reference that resolves to nothing is this configuration's most repeated
// bug: a connector that does not exist means no caching for ever, a validator
// that does not exist means the field is not checked, a flow that does not
// exist means an aspect that writes nothing. Each was found separately, and
// each was fixed separately, which is exactly how the next one gets in.
//
// The schema marks every reference with a Ref kind. This test walks them and
// fails if one is neither checked at startup nor written down here with the
// reason it is not — so a reference added later has to be dealt with rather
// than discovered.
func TestEveryReferenceInTheSchemaIsChecked(t *testing.T) {
	checked := map[schema.RefKind]string{
		schema.RefConnector: "ValidateConnectorReferences",
		schema.RefType:      "ValidateTypeReferences",
		schema.RefValidator: "ValidateValidatorReferences",
		schema.RefFlow:      "ValidateAuthHooks and ValidateAspectFlowReferences",
	}

	// The reusable-block kinds resolve in the parser, which refuses a name it
	// cannot find while folding the reference in — see resolveRef, whose error
	// names the kind and lists what is available. They need nothing here.
	resolvedByTheParser := map[schema.RefKind]bool{
		schema.RefCache:         true,
		schema.RefTransform:     true,
		schema.RefDedupe:        true,
		schema.RefRetry:         true,
		schema.RefLock:          true,
		schema.RefSemaphore:     true,
		schema.RefSequenceGuard: true,
		schema.RefCoordinate:    true,
		schema.RefTransaction:   true,
		schema.RefErrorHandling: true,
		schema.RefAccept:        true,
		schema.RefResponse:      true,
		schema.RefStateMachine:  true,
	}

	// The root blocks, and the field constraints — which are not a block and
	// are reached by no walk over them, which is how the one reference in
	// there went unnoticed while this test claimed to cover every reference in
	// the schema.
	all := []schemaRef{}
	for _, blk := range schema.BuiltinRootSchemas() {
		all = append(all, referencesIn(blk, blk.Type)...)
	}
	for _, a := range schema.FieldConstraints() {
		if a.Ref != schema.RefNone {
			all = append(all, schemaRef{where: "type.<field>." + a.Name, kind: a.Ref})
		}
	}

	var unchecked []string
	{
		for _, ref := range all {
			if checked[ref.kind] != "" || resolvedByTheParser[ref.kind] {
				continue
			}
			unchecked = append(unchecked, fmt.Sprintf("%s (%v)", ref.where, ref.kind))
		}
	}

	if len(unchecked) > 0 {
		sort.Strings(unchecked)
		t.Errorf("these point at something by name and nothing checks the name:\n  %s\n\n"+
			"Either add a check, or add the kind to resolvedByTheParser with the reason.",
			strings.Join(unchecked, "\n  "))
	}
}

type schemaRef struct {
	where string
	kind  schema.RefKind
}

func referencesIn(blk schema.Block, path string) []schemaRef {
	var refs []schemaRef

	for _, a := range blk.Attrs {
		if a.Ref != schema.RefNone {
			refs = append(refs, schemaRef{where: path + "." + a.Name, kind: a.Ref})
		}
	}
	for _, child := range blk.Children {
		refs = append(refs, referencesIn(child, path+"."+child.Type)...)
	}
	return refs
}
