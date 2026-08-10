package resource

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestExpandPathsResolveAgainstSDKStructs checks every path we advertise in
// `resource describe` against the pinned SDK's response structs.
//
// This guards a failure mode with no natural symptom: Stripe accepts an expand
// path for a field the SDK struct no longer models, returns the data, and our
// marshalling silently discards it. The caller sees `null` — no error, no
// warning, no hint that the recommendation is stale. An SDK bump that
// restructures a resource (as the Basil-era Invoice changes did) reintroduces
// this every time, so it needs a test rather than vigilance.
func TestExpandPathsResolveAgainstSDKStructs(t *testing.T) {
	for resource, paths := range expandPathsByResource {
		proto, ok := resourceRegistry[resource]
		if !ok {
			t.Errorf("%s has expand paths but no entry in resourceRegistry", resource)
			continue
		}
		for _, p := range paths {
			if err := resolveJSONPath(reflect.TypeOf(proto), p); err != nil {
				t.Errorf("%s: expand path %q does not resolve: %v", resource, p, err)
			}
		}
	}
}

// Every registered resource should have an entry in the expand-path map, even
// if empty — an omission reads as "nobody curated this" rather than "there is
// nothing worth expanding".
func TestEveryResourceHasAnExpandPathEntry(t *testing.T) {
	for name := range resourceRegistry {
		if _, ok := expandPathsByResource[name]; !ok {
			t.Errorf("%s is in resourceRegistry but missing from expandPathsByResource (use {} if nothing is worth expanding)", name)
		}
	}
}

// resolveJSONPath walks a dot-separated expand path against a struct type,
// matching each segment to a field's `json:` tag. Pointers and slices are
// transparent, so `lines.data.pricing.price_details.price` steps through the
// list wrapper the same way Stripe's own path does.
func resolveJSONPath(t reflect.Type, path string) error {
	cur := t
	for _, seg := range strings.Split(path, ".") {
		cur = deref(cur)
		if cur.Kind() != reflect.Struct {
			return fmt.Errorf("segment %q: %s is not a struct", seg, cur.Kind())
		}
		f, ok := fieldByJSONName(cur, seg)
		if !ok {
			return fmt.Errorf("no field with json tag %q on %s", seg, cur.Name())
		}
		cur = f.Type
	}
	return nil
}

// deref unwraps pointers, slices and arrays until it reaches a concrete type.
func deref(t reflect.Type) reflect.Type {
	for {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			t = t.Elem()
		default:
			return t
		}
	}
}

func fieldByJSONName(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			// Embedded SDK helper structs (APIResource, ListMeta) carry the
			// real fields on some list types.
			if f.Anonymous {
				if inner, ok := fieldByJSONName(deref(f.Type), name); ok {
					return inner, true
				}
			}
			continue
		}
		if strings.SplitN(tag, ",", 2)[0] == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}
