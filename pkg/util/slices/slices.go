package slices

import (
	"reflect"
	"slices"
	"sort"
)

// ContainsString checks if a []string slice contains a query string
func ContainsString(strings []string, query string) bool {
	return slices.Contains(strings, query)
}

// ContainsAnyString checks if an []interface{} slice contains a query string
func ContainsAnyString(strings any, query any) bool {
	return ContainsString(StringSlice(strings), query.(string))
}

// FilterString returns a copy of a slice with the items that return true when passed to `test`
func FilterString(ss []string, test func(string) bool) (ret []string) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}
	return
}

// StringSlice converts an []interface{} slice to a []string slice
func StringSlice(strings any) []string {
	if reflect.TypeOf(strings).Kind() != reflect.Slice {
		panic("strings is not a slice")
	}
	ret := []string{}
	vals := reflect.ValueOf(strings)
	for i := 0; i < vals.Len(); i++ {
		ret = append(ret, vals.Index(i).String())
	}
	return ret
}

// StringKeys returns the keys from a map[string]interface{} as a sorted []string slice
func StringKeys(m any) []string {
	keys := []string{}
	v := reflect.ValueOf(m)
	if v.Kind() == reflect.Map {
		for _, key := range v.MapKeys() {
			keys = append(keys, key.String())
		}
		sort.Strings(keys)
		return keys
	}
	panic("StringKeys received not a map")
}
