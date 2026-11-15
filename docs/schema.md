# Schema specification

A `.plug` file declares a service's package, version, permissions, types, methods, and HTTP bindings.
Field order determines encoding;
declaration order determines category membership.
Other source ordering does not affect the schema fingerprint.
The [protocol](protocol.md) defines identity and value encoding;
the [JSON export](schema-json.md) provides resolved metadata without requiring a schema parser.

## Package

The package declaration comes first:

```plug
// directory describes a user directory.
package directory version 1
```

The name is a simple identifier as defined below.
The version is a positive 16-bit decimal integer, defaulting to 1.
Package and version identify a compatibility family.
A breaking update requires a new version.

## Names

Names use ASCII letters, digits, and `_`, starting with a letter or `_`.
The blank name `_` and Go keywords are invalid.
Method and permission names have exactly two components,
such as `directory.getUser` and `user.read`, with no whitespace around the dot.
Referenced types and permissions must be declared.

The names `http`, `httpStatus`, `list`, `package`, `permission`, `public`,
`requires`, `throws`, `type`, and `version` are reserved in declarations and fields.
Package names are subject to the identifier rule, but not this additional reserved-name list.

### Go binding names

The Go tool checks generated-name collisions when validating a schema,
including when generating TypeScript or JSON.
These naming rules do not participate in wire encoding.

Go names split at `_` and `.`, uppercase each part's first letter, and join the parts.
Other letters retain their case, except for initialisms.
The initialisms `API`, `DC`, `HTTP`, `HTTPS`, `ID`, `IP`, `MIME`, `PNG`, `QR`,
`RPC`, `SQL`, `SSH`, `TCP`, `UDP`, `URL`, `UUID`, and `WAL` retain that capitalization.
This also applies to an initialism at the start of a part when followed by an uppercase letter.
For example, `user_id` becomes `UserID` and `httpServer` becomes `HTTPServer`.

Methods use only their operation component:
`directory.getUser` generates `GetUser`.
Generated names must be distinct within their Go scope.
This includes type and sum-constructor names, request types, and generated method metadata.

Bindings reserve `Schema`, `Client`, `Handlers`, `NewClient`, and `RegisterHandlers`,
plus `HTTPRoutes` when HTTP bindings are present.
Sums also generate `Unknown<Type>` types.
Non-raw methods cannot generate `Close`, which belongs to `Client.Close`.
Fields of application errors cannot generate `Error`, which is their error method.

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

`void` and `raw` are valid only as non-optional method responses.
A raw method cannot have an HTTP binding.

### Lists and optional values

`list<T>` is an ordered list of `T`.
Lists may be nested, but list elements cannot be optional.
Use `list<T>?` to make the entire list optional.

`T?` is an optional value.
Required fields must precede optional fields in every constructor and request.
This lets an updated schema append optional fields that older peers can ignore.

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

Its base must be `bool`, `bytes`, `f64`, `i32`, `i64`, `string`, `u32`, or `u64`.
It cannot wrap another newtype, a record, a sum, `void`, or `raw`.
Type names cannot replace primitive names.

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

One constructor for a result type defines a record.
Two or more define a sum:

```plug
// Role is a user's access role.
type Role

// admin grants administrator access.
admin level: u32 = Role

// guest grants guest access.
guest = Role
```

The optional `type Role` declaration supplies documentation for the result type.
It requires at least one constructor and does not change the encoding.

Constructor names are unique across the schema.
Field names are unique within their constructor or request.
Records and sums cannot refer back to themselves, directly or through other types or lists.

## Permissions and methods

A permission declares a name that restricted methods may require:

```plug
// user.read allows callers to read users.
permission user.read
```

A method lists request fields, a response type, and optional error, access, and HTTP clauses:

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

A method without an access clause is public;
`public` states the default explicitly.
`requires P` requires a principal authorized for the declared permission `P`.
Public methods skip this authorization check.
Transport authentication may still reject their connections.

A method may declare several HTTP bindings.
Servers expose every binding;
generated HTTP clients use the first in canonical verb-and-path order.

The `throws` type must be a record or sum.
An error type cannot also appear in fields or successful responses.
Only constructors of error types may declare `httpStatus`, from 400 through 599:

```plug
// notFound indicates that the user does not exist.
notFound id: UserID = LookupError httpStatus 404
```

Declared application errors are distinct from core failures,
such as authentication, authorization, decoding, or internal errors.

## Categories

A category groups the types and methods following it, up to the next category:

```plug
// Users groups user operations.
category "Users"
```

Categories organize generated documentation.
A category name must be nonempty and have no surrounding whitespace.
Each category must contain at least one type or method.
All declarations of a type must belong to the same category.
Declarations before the first category are uncategorized.

Categories and documentation do not affect IDs or fingerprints.

## HTTP bindings

An `http` clause gives a method an HTTP route:

```plug
http GET "/v1/users/{id}"
http POST "/v1/get-user"
```

See [HTTP](http.md) for route patterns, request mapping, JSON values, errors, and clients.
The clause affects the fingerprint, but not the method ID or native value encoding.

## Documentation and linting

Comments use [Go doc comment syntax], normalized by `go/doc/comment`.
It supports paragraphs, headings, lists, indented code blocks, and URL links.
Use indented code rather than Markdown fences;
backticks, emphasis, and raw HTML have no special formatting meaning.

Separate paragraphs with a blank `//` line.
Indentation after `//` matters:

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

Exact type references, such as `[User]`, and qualified method references,
such as `[users.get]`, link to declarations in the browser and Markdown download.
Unknown references remain literal text.
Explicit URL definitions take precedence.
Constructors, fields, and permissions are not automatic link targets.

Canonical source, inspection, and JSON exports preserve normalized comment syntax and references.
Generated Go and TypeScript comments replace schema references with plain names;
URL links and preformatted text retain their syntax.

Documentation is optional in the language.
`plug lint` and `plug pre-commit` report missing comments and unused permissions and types.
No fixed prose or punctuation is required.

Start a comment with the declaration's name when it fits.
For example, `users.get returns the requested user.` becomes `Get returns the requested user.` in Go.
Other prose follows a short generated declaration comment.

[Go doc comment syntax]: https://go.dev/doc/comment

## Canonical form

Canonical source is a deterministic, formatted schema returned by inspection and JSON export.
Its ordering is:

1. Package with an explicit version.
2. Permissions sorted by name.
3. Uncategorized newtypes sorted by name.
4. Uncategorized records and sums sorted by result type name.
5. Uncategorized methods sorted by name.
6. Categories in source order, each with types sorted by name followed by methods sorted by name.

Within a record or sum, constructors are sorted by name.
An explicit `type Name` documentation declaration precedes its constructors.
Field order is unchanged.
HTTP bindings are sorted by verb, then path.
Sorting compares names and paths by their UTF-8 bytes.

Canonical source omits semicolons and ends in a newline.
Declarations are separated by a blank line.
Without field comments, a constructor or method and its fields occupy one line.
If any field has documentation, the declaration name occupies a line,
each field and its comments are indented by two spaces,
and `= Type` or `-> Type` occupies the next unindented line.

A method with only an access clause puts it after the response type.
Otherwise, its clauses follow on separate lines indented by two spaces:
`throws` first, then access, then HTTP bindings.
Strings use the [canonical quoting](protocol.md#quoted-paths) rules.
Comments retain Go doc formatting, with `//` markers restored.

Canonical source includes documentation and categories.
The fingerprint instead uses the separate [identity text](protocol.md#schema-identity),
which excludes them and has different spacing and ordering.

## Compatible evolution

Compatibility is directional:
a new schema is an additive replacement for an old schema in the same package and version.

A compatible update may:

- add methods, types, permissions, categories, and HTTP bindings;
- add a constructor to a type that is already a sum; and
- append optional fields to requests, records, sums, and application errors.

It must preserve existing fields' names, types, and order;
existing methods' response and error types, access rules, and HTTP bindings;
and existing newtype bases, constructors, permissions, and error HTTP statuses.
It cannot turn a record into a sum or add a required field to an existing request or constructor.
Documentation and category membership can change without affecting wire identity.

Older clients can keep calling existing methods on an updated server.
An older server does not acquire newly added methods;
calling one produces `Unimplemented`.
Unknown sum constructors and optional tails let older peers read extended values.

A matching family hash does not prove that an update follows these rules.
The default handshake checks the family;
strict matching also checks the fingerprint and therefore rejects additive changes.
Check an update before deploying it:

```sh
$ go tool plug check old.plug new.plug
```

Breaking changes require a new package version.
See the [greeter schema](../example/greeter/schema.plug) for a larger example.

Schemas have no imports, maps, recursive types, defaults, or built-in time type.

## Grammar

The EBNF below describes syntax;
the preceding sections impose semantic constraints.

- Single-quoted text is literal.
- `X | Y` means either alternative.
- `[X]` means zero or one occurrence.
- `{X}` means zero or more occurrences.
- Parentheses group elements.
- A period ends a production.

### Lexical elements

Spaces, tabs, carriage returns, and newlines separate tokens but are otherwise insignificant.
Identifiers and keywords are case-sensitive.
A qualified name is one token, with no whitespace around its dot.

```ebnf
name          = letter {letter | digit | '_'} .
qualifiedName = name '.' name .
number        = digit {digit} .
comment       = '//' {character except newline} .

letter = 'A' ... 'Z' | 'a' ... 'z' | '_' .
digit  = '0' ... '9' .
```

A `string` is a Go double-quoted string literal, including Go escape sequences.
It cannot contain an unescaped newline.
Punctuation tokens are `:`, `;`, `=`, `?`, `<`, `>`, and `->`.

Consecutive comments form documentation for the following declaration or field.
Nonempty documentation not followed by a declaration or field is invalid.
Semicolons are optional after the package and each declaration.

### Declarations

```ebnf
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

A method has at most one access clause.
`throws` precedes all method clauses;
HTTP and access clauses may otherwise be interleaved.
The Go tool reports the first schema error with its file, line, and byte column.
