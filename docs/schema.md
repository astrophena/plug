# Schema specification

A `.plug` file declares a service's package, version, permissions, types,
errors, methods, and HTTP bindings. Field order determines encoding; declaration
order determines category membership. Other source ordering does not affect the
schema fingerprint. See the [protocol](protocol.md#value-encoding) for value
encoding.

## Package

The package declaration is first. Its name must be a valid, non-blank Go
identifier. The version is a positive 16-bit decimal integer and defaults to 1.

```plug
// directory describes a user directory.
package directory version 1
```

The package and version identify a compatibility family. A breaking update uses
a new version.

## Names

Simple declaration and field names are Go identifiers other than `_`. Method and
permission names have exactly two nonempty components, such as
`directory.getUser` and `user.read`. Referenced types and permissions must be
declared.

The names `http`, `httpStatus`, `list`, `package`, `permission`, `public`,
`requires`, `throws`, `type`, and `version` are reserved.

Generated Go names use title case, remove `_` and `.`, and preserve common
initialisms such as `ID`, `HTTP`, `SQL`, and `URL`. Method declarations use only
the operation component: `directory.getUser` generates `GetUser`. Generated
declarations must have distinct names within their Go scope.

Generated bindings reserve `Schema`, `Client`, `Handlers`, `NewClient`, and
`RegisterHandlers`, plus `HTTPRoutes` when HTTP bindings are present. Non-raw
methods cannot generate `Close`, which is used by `Client.Close`.

## Types

### Primitive types

| Type     | Meaning                 |
| -------- | ----------------------- |
| `bool`   | Boolean                 |
| `bytes`  | Byte sequence           |
| `f64`    | IEEE 754 binary64       |
| `i32`    | Signed 32-bit integer   |
| `i64`    | Signed 64-bit integer   |
| `u32`    | Unsigned 32-bit integer |
| `u64`    | Unsigned 64-bit integer |
| `string` | UTF-8 string            |
| `void`   | No response value       |
| `raw`    | Connection takeover     |

`void` and `raw` are valid only as non-optional method responses. A raw method
cannot have an HTTP binding.

### Lists and optional values

`list<T>` is an ordered list of `T`. Lists may be nested. A list element cannot
be optional; use `list<T>?` to make the entire list optional.

`T?` is an optional value. Required fields must precede optional fields in a
constructor or request. This lets an updated schema append optional fields that
older peers can ignore.

```plug
labels: list<string>
email: string?
```

### Newtypes

A newtype defines a distinct named scalar:

```plug
// UserID identifies a user.
type UserID = i64
```

Its base must be one of `bool`, `bytes`, `f64`, `i32`, `i64`, `string`, `u32`,
or `u64`. Newtypes cannot wrap other newtypes, records, sums, `void`, or `raw`.

### Records and sums

A constructor lists fields and a result type:

```plug
// user describes a user.
user
  // id identifies the user.
  id: UserID
  // name is the user's display name.
  name: string
= User
```

Exactly one constructor for a result type defines a record. Two or more
constructors with the same result type define a sum:

```plug
// Role is a user's access role.
type Role

// admin grants administrator access.
admin level: u32 = Role

// guest grants guest access.
guest = Role
```

The optional `type Role` declaration supplies documentation for the result type.
It neither creates a type by itself nor changes its encoding.

Constructor names are unique across the schema. Field names are unique within
their constructor. Recursive record and sum graphs are invalid.

## Permissions and methods

A permission declares a name that restricted methods may require:

```plug
// user.read allows callers to read users.
permission user.read
```

A method contains request fields, a response type, and optional error, access,
and HTTP clauses:

```plug
// directory.getUser returns the requested user.
directory.getUser
  // id identifies the user to return.
  id: UserID
-> User
  throws LookupError
  requires user.read
  http GET "/v1/users/{id}"
```

A method with no access clause is public. `public` states that default
explicitly. `requires P` restricts the method to callers authorized for the
declared permission `P`.

A method may declare several HTTP bindings. Servers expose every binding.
Generated HTTP clients use the first binding in canonical verb-and-path order.

The type named by `throws` must be a record or sum type. Once used as a method
error, that type cannot also appear in fields or successful responses.
Constructors of an error type may declare an HTTP status from 400 through 599:

```plug
// notFound indicates that the user does not exist.
notFound id: UserID = LookupError httpStatus 404
```

The runtime distinguishes a declared application error from authentication,
authorization, decoding, and internal failures.

## Categories

A category groups the types and methods following it, up to the next category:

```plug
// Users groups user operations.
category "Users"
```

Categories group declarations in generated documentation. A category name must
be nonempty, have no surrounding whitespace, and contain at least one type or
method. A declaration cannot belong to two categories. Declarations before the
first category are uncategorized.

Categories and their documentation do not affect IDs or fingerprints.

## HTTP bindings

An `http` clause gives a method an HTTP route:

```plug
http GET "/v1/users/{id}"
http POST "/v1/get-user"
```

See [HTTP](http.md) for route patterns, request mapping, JSON values, errors,
and clients. The `http` clause does not change the method ID or native encoding.

## Documentation and linting

Documentation comments use [Go doc comment syntax] and are normalized by
`go/doc/comment`. Paragraphs, headings, lists, indented code blocks, and URL
links are supported. Use indented blocks for code rather than Markdown fences;
backticks, emphasis, and raw HTML have no special formatting meaning.

Write a blank `//` line between paragraphs. Indentation after `//` is
significant for lists and code blocks:

```plug
// users.get returns the requested [User].
//
// # Lookup
//
// The caller supplies a stable identifier:
//
//     users.get id: 123
//
// See [the guide].
//
// [the guide]: https://example.com/guide
users.get id: i64 -> User
```

Bracketed references to exact schema type names, such as `[User]`, and qualified
method names, such as `[users.get]`, link to those declarations in the HTML
browser and Markdown download. Unknown references remain literal text. Explicit
URL link definitions take precedence over schema references. Constructors,
fields, and permissions are not automatic link targets.

Canonical schemas, schema inspection, and JSON exports preserve references and
normalized comment syntax. Generated Go and TypeScript comments replace schema
references with plain names: `[users.get]` becomes `users.get`. URL links and
preformatted text retain their syntax. Documentation and categories do not
affect wire identity.

The language permits missing documentation. `plug lint` and `plug pre-commit`
report declarations and fields without comments, along with unused permissions
and types. Comments can use natural prose; no fixed phrases or punctuation are
required.

Start a comment with the schema declaration's name when it fits. For example,
`users.get returns the requested user.` becomes
`Get returns the requested user.` in generated Go. Other prose follows a short
generated declaration comment.

[Go doc comment syntax]: https://go.dev/doc/comment

## Canonical form

Canonical formatting is deterministic. It:

- writes the package and explicit version first;
- sorts permissions and uncategorized declarations by name;
- retains category order while sorting declarations within each category;
- sorts HTTP bindings by verb and path;
- omits semicolons; and
- preserves normalized documentation blocks.

Canonical source is generated metadata and is returned by inspection. Cosmetic
source ordering and whitespace therefore do not affect the exact fingerprint.

## Compatible evolution

Two schemas are compatible when they have the same package and version and the
new schema is an additive replacement for the old schema.

A compatible update may:

- add methods, types, permissions, categories, and HTTP bindings;
- add a constructor to a type that is already a sum; and
- append optional fields to requests, records, sums, and application errors.

A compatible update does not remove or alter an existing declaration, field,
permission, access rule, HTTP binding, or error status. It does not turn a
record into a sum. Adding a required field to an existing value is incompatible.

Unknown sum constructors and optional tails make these additions readable by
older peers. Breaking changes require a new package version.

Check an update with:

```sh
$ go tool plug check old.plug new.plug
```

See the [greeter schema](../example/greeter/schema.plug) for a complete example.

Schemas have no imports, maps, recursion, defaults, or built-in time type. The
[protocol](protocol.md#schema-identity) defines wire IDs and hashes. The
[schema JSON export](schema-json.md) describes resolved metadata for other
tools.

## Grammar

The grammar uses this notation:

- lowercase names denote lexical tokens;
- `'quoted'` text denotes a literal token;
- `X | Y` means either `X` or `Y`;
- `[X]` means zero or one `X`;
- `{X}` means zero or more `X` values;
- parentheses group grammar elements; and
- each production ends with a period.

### Lexical elements

A schema is text containing whitespace, identifiers, decimal integers, quoted
strings, punctuation, and documentation comments. Spaces, tabs, carriage
returns, and newlines separate tokens but are otherwise insignificant.

```abnf
name          = letter {letter | digit | '_'} .
qualifiedName = name '.' name .
number        = digit {digit} .
comment       = '//' {character except newline} .

letter = 'A' ... 'Z' | 'a' ... 'z' | '_' .
digit  = '0' ... '9' .
```

Identifiers and keywords are case-sensitive. A quoted string uses Go
double-quoted string syntax, including its escape sequences. The punctuation
tokens are `:`, `;`, `=`, `?`, `<`, `>`, and `->`.

Every comment is documentation for the declaration or field immediately after
it. Consecutive comments form a documentation block. A documentation block not
followed by a declaration or field is invalid.

Semicolons are optional after the package declaration and each declaration.
Canonical schemas omit them.

### Declarations

```abnf
Schema = Documentation PackageDecl {Documentation Declaration} .

PackageDecl = 'package' name ['version' number] [';'] .

Declaration = PermissionDecl
            | NewtypeDecl
            | TypeDecl
            | ConstructorDecl
            | CategoryDecl
            | MethodDecl
            .

PermissionDecl = 'permission' qualifiedName [';'] .
NewtypeDecl    = 'type' name '=' scalar [';'] .
TypeDecl       = 'type' name [';'] .
CategoryDecl   = 'category' string [';'] .

ConstructorDecl = name {DocumentedField} '=' name
                  ['httpStatus' number] [';'] .

MethodDecl = qualifiedName {DocumentedField} '->' Type
             ['throws' name] {MethodClause} [';'] .

MethodClause = 'public'
             | 'requires' qualifiedName
             | 'http' httpVerb string
             .

DocumentedField = Documentation Field .
Field           = name ':' Type .

Type        = PrimaryType ['?'] .
PrimaryType = scalar | name | 'list' '<' Type '>' .

scalar = 'bool' | 'bytes' | 'f64'
       | 'i32' | 'i64' | 'u32' | 'u64'
       | 'string' | 'void' | 'raw'
       .

httpVerb      = 'DELETE' | 'GET' | 'PATCH' | 'POST' | 'PUT' .
Documentation = {comment} .
```

A method may contain at most one access clause. The `throws` clause, when
present, precedes all method clauses. HTTP and access clauses may otherwise be
interleaved. Invalid schemas report the first error with its file, line, and
byte column.
