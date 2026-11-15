// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"crypto/sha256"
	"fmt"
	"hash/crc32"
	"slices"
	"strconv"
	"strings"
)

// IDs use required fields.
// Fingerprints also include optional fields, access rules, and HTTP metadata.
func constructorText(pkg string, c constructor, full bool) string {
	var b strings.Builder
	b.WriteString(pkg)
	b.WriteByte('.')
	b.WriteString(c.name)
	writeFields(&b, c.fields, full)
	b.WriteString(" = ")
	b.WriteString(c.result)
	if full && c.httpStatus != 0 {
		b.WriteString(" httpStatus ")
		b.WriteString(strconv.Itoa(c.httpStatus))
	}
	return b.String()
}

func methodText(m method, full bool) string {
	var b strings.Builder
	b.WriteString(m.name)
	writeFields(&b, m.fields, full)
	b.WriteString(" -> ")
	b.WriteString(m.response.String())
	if m.errorType != nil {
		b.WriteString(" throws ")
		b.WriteString(m.errorType.String())
	}
	if !full {
		return b.String()
	}

	if m.public {
		b.WriteString(" public")
	} else {
		b.WriteString(" requires ")
		b.WriteString(m.permission)
	}
	for _, binding := range sortedHTTP(m.http) {
		b.WriteString(" http ")
		b.WriteString(binding.verb)
		b.WriteByte(' ')
		b.WriteString(strconv.Quote(binding.path))
	}
	return b.String()
}

func writeFields(b *strings.Builder, fields []field, full bool) {
	for _, f := range fields {
		if f.typ.optional && !full {
			break
		}
		b.WriteByte(' ')
		b.WriteString(f.name)
		b.WriteByte(':')
		b.WriteString(f.typ.String())
	}
}

// fingerprint computes the normalized wire-schema fingerprint.
func fingerprint(s *Schema) [sha256.Size]byte {
	decls := []string{fmt.Sprintf("package %s version %d", s.packageName, s.major)}
	for _, p := range s.permissions {
		decls = append(decls, "permission "+p.name)
	}
	for _, value := range s.newtypes {
		decls = append(decls, fmt.Sprintf("type %s.%s = %s", s.packageName, value.name, value.base))
	}
	for _, c := range s.constructors {
		decls = append(decls, constructorText(s.packageName, c, true))
	}
	for _, m := range s.methods {
		decls = append(decls, methodText(m, true))
	}
	slices.Sort(decls[1:])
	return sha256.Sum256([]byte(strings.Join(decls, "\n")))
}

func assignIDs(s *Schema) error {
	ids := make(map[uint32]string, len(s.constructors)+len(s.methods))
	assign := func(canonical string, pos Position) (uint32, error) {
		id := crc32.ChecksumIEEE([]byte(canonical))
		if previous, ok := ids[id]; ok {
			return 0, diagnostic(pos, "CRC32 collision between %q and %q (0x%08x)", previous, canonical, id)
		}
		ids[id] = canonical
		return id, nil
	}

	for i := range s.constructors {
		c := &s.constructors[i]
		id, err := assign(constructorText(s.packageName, *c, false), c.position)
		if err != nil {
			return err
		}
		c.id = id
	}
	for i := range s.methods {
		m := &s.methods[i]
		id, err := assign(methodText(*m, false), m.position)
		if err != nil {
			return err
		}
		m.id = id
	}
	return nil
}
