package api

import (
	"fmt"
	"reflect"
	"sync"
)

// secretTag is the struct tag, roost:"secret", on a response field that only
// admins get. writeJSON clears such fields for everyone else, so handlers
// never need to check who's asking before they answer.
const secretTag = "secret"

// redact returns v with every field tagged roost:"secret" set to its zero
// value, wherever it's nested: in structs, pointers, slices, arrays, map
// values and interfaces. It copies what it changes and never changes v, which
// may be shared, such as the poller's latest snapshot. It returns v as it is
// if its type can't hold a secret.
func redact[T any](v T) T {
	rv := reflect.ValueOf(&v).Elem()
	if !holdsSecrets(rv.Type()) {
		return v
	}
	var out T
	reflect.ValueOf(&out).Elem().Set(redactValue(rv))
	return out
}

func redactValue(v reflect.Value) reflect.Value {
	t := v.Type()
	if !holdsSecrets(t) {
		return v
	}
	switch t.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		p := reflect.New(t.Elem())
		p.Elem().Set(redactValue(v.Elem()))
		return p
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(t).Elem()
		out.Set(redactValue(v.Elem()))
		return out
	case reflect.Struct:
		out := reflect.New(t).Elem()
		out.Set(v)
		for i := range t.NumField() {
			f := t.Field(i)
			switch {
			case !f.IsExported():
				// holdsSecrets made sure there's nothing to clear here.
			case f.Tag.Get("roost") == secretTag:
				out.Field(i).SetZero()
			default:
				out.Field(i).Set(redactValue(v.Field(i)))
			}
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(t, v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(redactValue(v.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(t).Elem()
		for i := range v.Len() {
			out.Index(i).Set(redactValue(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(t, v.Len())
		for k, e := range v.Seq2() {
			out.SetMapIndex(k, redactValue(e))
		}
		return out
	default:
		return v
	}
}

// secretTypes caches holdsSecrets for each type it's asked about.
var secretTypes sync.Map // reflect.Type → bool

// holdsSecrets reports whether a value of type t can hold a field tagged
// roost:"secret". An interface can hold anything, so it always can. It
// panics if a secret is in a struct embedded without being exported, since
// JSON encodes that struct's fields but redact can't clear them.
func holdsSecrets(t reflect.Type) bool {
	if known, ok := secretTypes.Load(t); ok {
		holds, _ := known.(bool)
		return holds
	}
	holds := findSecrets(t, map[reflect.Type]bool{})
	secretTypes.Store(t, holds)
	return holds
}

// findSecrets is holdsSecrets without the cache. seen holds the types it's
// already looking into, so a recursive type doesn't recurse forever: the
// first visit gives the answer.
func findSecrets(t reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Interface:
		return true
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return findSecrets(t.Elem(), seen)
	case reflect.Struct:
		holds := false
		for f := range t.Fields() {
			secret := f.Tag.Get("roost") == secretTag
			switch {
			case f.IsExported():
				holds = holds || secret || findSecrets(f.Type, seen)
			case f.Anonymous && (secret || findSecrets(f.Type, seen)):
				panic(fmt.Sprintf("api: %s embeds %s, which holds a secret, so export it", t, f.Type))
			}
		}
		return holds
	default:
		return false
	}
}
