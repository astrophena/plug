// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// CheckCompatibility verifies that newer is an additive replacement for older.
func CheckCompatibility(older, newer *Schema) error {
	if older == nil || newer == nil {
		return errors.New("schema is nil")
	}

	var changes []string
	if older.packageName != newer.packageName || older.major != newer.major {
		changes = append(changes, fmt.Sprintf(
			"package generation changed from %s version %d to %s version %d",
			older.packageName,
			older.major,
			newer.packageName,
			newer.major,
		))
	}

	newtypes := make(map[string]newtype, len(newer.newtypes))
	for _, value := range newer.newtypes {
		newtypes[value.name] = value
	}
	for _, old := range older.newtypes {
		cur, ok := newtypes[old.name]
		if !ok {
			changes = append(changes, "removed newtype "+old.name)
		} else if cur.base != old.base {
			changes = append(changes, fmt.Sprintf("changed newtype %s from %s to %s", old.name, old.base, cur.base))
		}
	}

	var (
		constructors = make(map[string]constructor, len(newer.constructors))
		oldGroups    = groupConstructors(older)
		newGroups    = groupConstructors(newer)
	)
	for result, oldGroup := range oldGroups {
		if len(oldGroup) == 1 && len(newGroups[result]) > 1 {
			changes = append(changes, "changed record "+result+" into a sum")
		}
	}
	for _, c := range newer.constructors {
		constructors[c.name] = c
	}
	for _, old := range older.constructors {
		cur, ok := constructors[old.name]
		if !ok {
			changes = append(changes, "removed constructor "+old.name)
			continue
		}
		if old.result != cur.result {
			changes = append(changes, fmt.Sprintf("changed constructor %s result from %s to %s", old.name, old.result, cur.result))
		}
		changes = append(changes, incompatibleFields("constructor "+old.name, old.fields, cur.fields)...)
		if old.httpStatus != cur.httpStatus {
			changes = append(changes, fmt.Sprintf("changed constructor %s HTTP status from %d to %d", old.name, old.httpStatus, cur.httpStatus))
		}
	}

	methods := make(map[string]method, len(newer.methods))
	for _, m := range newer.methods {
		methods[m.name] = m
	}
	for _, old := range older.methods {
		cur, ok := methods[old.name]
		if !ok {
			changes = append(changes, "removed method "+old.name)
			continue
		}
		if methodText(old, false) != methodText(cur, false) {
			changes = append(changes, "changed required signature of method "+old.name)
		}
		changes = append(changes, incompatibleFields("method "+old.name, old.fields, cur.fields)...)
		if old.public != cur.public || old.permission != cur.permission {
			changes = append(changes, "changed access requirement of method "+old.name)
		}
		for _, binding := range old.http {
			if !slices.ContainsFunc(cur.http, func(value httpBinding) bool {
				return binding.verb == value.verb && binding.path == value.path
			}) {
				changes = append(changes, fmt.Sprintf("removed HTTP binding %s %s from method %s", binding.verb, binding.path, old.name))
			}
		}
	}

	permissions := make(map[string]bool, len(newer.permissions))
	for _, p := range newer.permissions {
		permissions[p.name] = true
	}
	for _, p := range older.permissions {
		if !permissions[p.name] {
			changes = append(changes, "removed permission "+p.name)
		}
	}

	if len(changes) == 0 {
		return nil
	}
	slices.Sort(changes)
	return fmt.Errorf("incompatible schema changes:\n- %s", strings.Join(changes, "\n- "))
}

func incompatibleFields(owner string, older, newer []field) []string {
	if len(newer) < len(older) {
		return []string{fmt.Sprintf("removed fields from %s", owner)}
	}
	var changes []string
	for i, old := range older {
		cur := newer[i]
		if old.name != cur.name || old.typ.String() != cur.typ.String() {
			changes = append(changes, fmt.Sprintf("changed field %d of %s from %s: %s to %s: %s", i+1, owner, old.name, old.typ, cur.name, cur.typ))
		}
	}
	for _, f := range newer[len(older):] {
		if !f.typ.optional {
			changes = append(changes, fmt.Sprintf("added required field %s to %s", f.name, owner))
		}
	}
	return changes
}
