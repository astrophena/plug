// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import "fmt"

// Position identifies a source location.
type Position struct {
	// Filename is the parsed schema path.
	Filename string
	// Line is the one-based source line.
	Line int
	// Column is the one-based byte column.
	Column int
}

// String returns a go-tool-compatible source position.
func (p Position) String() string {
	if p.Filename == "" {
		return fmt.Sprintf("%d:%d", p.Line, p.Column)
	}
	return fmt.Sprintf("%s:%d:%d", p.Filename, p.Line, p.Column)
}

// Error is a schema diagnostic with a source position.
type Error struct {
	// Position is the location that produced the diagnostic.
	Position Position
	// Message describes the invalid schema construct.
	Message string
}

// Error returns the source position and diagnostic message.
func (e *Error) Error() string { return e.Position.String() + ": " + e.Message }

func diagnostic(position Position, format string, values ...any) error {
	return &Error{
		Position: position,
		Message:  fmt.Sprintf(format, values...),
	}
}
