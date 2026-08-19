package parser

import (
	"reflect"
	"testing"
)

// Merging what two files declared.
//
// A configuration directory is read file by file and merged, so every list a
// Configuration holds has to be carried across. Merge is a hand-written list of
// appends, one per field, which is the shape that goes quietly out of date: a
// field added to the struct and forgotten here is dropped for every project
// with more than one file — which is every real one — and nothing says so.
//
// Rather than compare two lists by eye, this fills every slice on two
// configurations and checks that the merge kept both sides.
func TestEveryListSurvivesAMerge(t *testing.T) {
	left, right := &Configuration{}, &Configuration{}

	filled := fillSlices(t, left)
	fillSlices(t, right)
	if filled == 0 {
		t.Fatal("no slice fields found, so this test is checking nothing")
	}

	left.Merge(right)

	value := reflect.ValueOf(left).Elem()
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		if !field.IsExported() || value.Field(i).Kind() != reflect.Slice {
			continue
		}
		if got := value.Field(i).Len(); got != 2 {
			t.Errorf("%s holds %d after merging one from each side — it is missing from Merge, "+
				"so whatever a second file declares there is dropped", field.Name, got)
		}
	}
}

// fillSlices puts one zero element in every exported slice field, and reports
// how many it filled.
func fillSlices(t *testing.T, config *Configuration) int {
	t.Helper()

	value := reflect.ValueOf(config).Elem()
	filled := 0
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		if !field.IsExported() || value.Field(i).Kind() != reflect.Slice {
			continue
		}
		one := reflect.MakeSlice(field.Type, 1, 1)
		value.Field(i).Set(one)
		filled++
	}
	return filled
}
