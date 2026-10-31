// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package names converts Plug identifiers to Go and TypeScript names.
package names

import "strings"

var initialisms = map[string]string{
	"api":   "API",
	"dc":    "DC",
	"http":  "HTTP",
	"https": "HTTPS",
	"id":    "ID",
	"ip":    "IP",
	"mime":  "MIME",
	"png":   "PNG",
	"qr":    "QR",
	"rpc":   "RPC",
	"sql":   "SQL",
	"ssh":   "SSH",
	"tcp":   "TCP",
	"udp":   "UDP",
	"url":   "URL",
	"uuid":  "UUID",
	"wal":   "WAL",
}

// Go maps a schema identifier to an exported Go identifier.
func Go(name string) string {
	parts := strings.FieldsFunc(name, func(value rune) bool {
		return value == '_' || value == '.'
	})
	for i, part := range parts {
		parts[i] = goPart(part)
	}
	return strings.Join(parts, "")
}

func goPart(part string) string {
	if replacement, ok := initialisms[strings.ToLower(part)]; ok {
		return replacement
	}
	for initialism, replacement := range initialisms {
		if len(part) > len(initialism) &&
			strings.EqualFold(part[:len(initialism)], initialism) &&
			part[len(initialism)] >= 'A' && part[len(initialism)] <= 'Z' {
			return replacement + part[len(initialism):]
		}
	}
	if part == "" {
		return ""
	}
	return strings.ToUpper(part[:1]) + part[1:]
}

// GoMethod maps the method part of a qualified name to a Go identifier.
func GoMethod(name string) string {
	_, method, ok := strings.Cut(name, ".")
	if !ok {
		return Go(name)
	}
	return Go(method)
}

// TypeScriptMethod maps the method part of a qualified name to a TypeScript identifier.
func TypeScriptMethod(name string) string {
	goName := GoMethod(name)
	if goName == "" {
		return ""
	}

	for init, repl := range initialisms {
		if len(goName) == len(init) && strings.EqualFold(goName, init) {
			return strings.ToLower(goName)
		}
		if len(goName) > len(init) && strings.EqualFold(goName[:len(init)], init) &&
			goName[len(init)] >= 'A' && goName[len(init)] <= 'Z' {
			return strings.ToLower(repl) + goName[len(init):]
		}
	}

	return strings.ToLower(goName[:1]) + goName[1:]
}
