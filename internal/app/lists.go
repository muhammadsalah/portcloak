// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import "reflect"

// The frontend treats every list-valued field as a list. A Go slice that was
// never appended to is nil, and `encoding/json` writes nil as `null`, so an
// empty configuration handed the screens `"environments": null` — and the very
// first thing a screen does with a list is read its length or iterate it. That
// threw before the view could replace its spinner, which is why an empty
// PortCloak sat on "Loading configuration…" forever.
//
// Initialising each field at its construction site would work until the next
// field is added, and the fields that hurt most are the nested ones nobody
// remembers: a job's phases, a snapshot's realms. So the rule is enforced at
// the boundary instead, once, for whatever shape a controller returns:
//
//	an empty list crosses the bridge as [], and an empty map as {}.
//
// It matters on the way out to a terminal too: these same structs are what
// `pcloak --json` prints, and a consumer piping them into jq has the same
// trouble with a null where a list was promised as a screen does.
//
// TestControllers_NeverHandTheFrontendNull drives every screen-load method
// against an empty home and fails on the first null it finds.
func lists[T any](v T) T {
	rv := reflect.ValueOf(&v).Elem()
	fill(rv)
	return v
}

// fill walks an addressable value and replaces every nil slice and map it can
// set with an empty one.
func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.Slice:
		if v.IsNil() {
			if v.CanSet() {
				v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			}
			return
		}
		for i := range v.Len() {
			fill(v.Index(i))
		}

	case reflect.Map:
		if v.IsNil() {
			if v.CanSet() {
				v.Set(reflect.MakeMap(v.Type()))
			}
			return
		}
		// Map values are not addressable, so each one is filled in a temporary
		// and written back under its key.
		for _, key := range v.MapKeys() {
			tmp := reflect.New(v.Type().Elem()).Elem()
			tmp.Set(v.MapIndex(key))
			fill(tmp)
			v.SetMapIndex(key, tmp)
		}

	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			fill(v.Elem())
		}

	case reflect.Struct:
		for i := range v.NumField() {
			// Unexported fields — time.Time's wall clock, a mutex — are not
			// settable and are not marshalled either.
			if f := v.Field(i); f.CanSet() {
				fill(f)
			}
		}
	}
}
