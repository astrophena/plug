# Purl and PlugScript

Purl reads a server's schema and calls its methods. PlugScript is the expression
language used at the prompt and in `.purl` files.

Install Purl as described in the [README](../README.md#tools). With the server
from [Using Plug](intro.md) running:

```sh
$ purl 127.0.0.1:9000
```

At the prompt, enter a method name to read its documentation. Add parentheses
and named arguments to call it:

```purl
greeting.greet
greeting.greet(name: "Ada")
```

Tab completes methods and arguments. Use `:help` to read your server's API
documentation, or `purl -help` for endpoints, flags, and keyboard controls.
Raw methods are not supported.

After Purl detects a lost connection, it reconnects before the next call. The
server must still have the same schema. A failed call is never retried
automatically because it may already have changed server state.

## Values

PlugScript has strings, booleans, integers, floats, nil, lists, and records:

```purl
"hello"
true
42
2.5
nil
[1, 2, 3]
{name: "Ada", active: true}
```

`null` is an alias for `nil`. Lists use zero-based indexes. Record fields can be
read with a dot or string index:

```purl
let names = ["Ada", "Bob"]
names[0] // "Ada"
let person = {name: "Ada", active: true}
person.name // "Ada"
person["name"] // "Ada"
```

Arithmetic uses `+`, `-`, `*`, `/`, and `%`. Compare values with `==`, `!=`,
`<`, `<=`, `>`, and `>=`. Boolean operators are `&&`, `||`, and `!`; `&&` and
`||` stop when the result is known. Integers stay exact beyond machine integer
sizes. Comments use `//` or `/* ... */`.

## Bindings

`let` names an expression without evaluating it. The first use evaluates the
expression and saves its result:

```purl
let message = greeting.greet(name: "Ada")
message
message | count
```

Reading `message` makes one RPC call. `message | count` counts the characters in
the saved string without making another call. If a deferred expression changes
server state, that change happens on first use too.

Errors are saved in the same way, including cancellation. Rebind the expression
to try again.

Rebinding a name does not change existing bindings:

```purl
let x = 1
let saved = x
let x = 2
saved
```

`saved` is 1 because it uses `x` from the scope where it was defined.

## Functions

Define functions with `=>`. Each call evaluates the body:

```purl
let twice = x => x * 2
twice(3) // 6
let add = (a, b) => a + b
add(2, 3) // 5
```

Put an RPC expression in a function when you want each use to make another
call:

```purl
let refresh = () => greeting.greet(name: "Ada")
refresh()
refresh()
```

Each `refresh()` calls `greeting.greet` again.

Functions retain their defining scope. Recursion needs an explicit self
argument:

```purl
let done = (self, n) => n == 0 || self(self, n - 1)
done(done, 100000)
```

Tail calls reuse the evaluator's stack, so this example can run past the usual
256-level evaluation limit. The expression timeout and Ctrl-C still stop it.

## Pipelines and builtins

`x | f(y)` means `f(x, y)`. Parentheses can be omitted when there are no other
arguments:

```purl
let rows = [{name: "Ada", size: 3}, {name: "Bob", size: 5}]
rows | filter(r => r.size > 3) | map(r => r.name)
rows | sortBy(r => r.name) | take(1)
rows | map(r => r.size) | sum
rows | count
```

The first pipeline keeps the rows whose `size` is greater than 3, then extracts
their names. Its result is `["Bob"]`. Each function receives a complete value;
pipelines do not stream results or fetch more pages.

Arguments are evaluated before the function runs. Use `:help` to list builtins,
or `:help NAME` to read a builtin's arguments and behavior.

## Results

`_` holds the complete last successful result. Save it before another successful
expression replaces it:

```purl
rows
let result = _
result[0].name
```

The prompt shows lists of similarly shaped records as tables when they fit. The
`#` column is the list index. Long cells may be shortened, so use `_[0]` or a
saved result to read a full row. If the records have different fields or the
terminal is too narrow, the prompt displays ordinary records instead.

These display choices leave the values unchanged and make no RPC calls.

Long output uses a pager. Without one, large results are shortened. Timestamps
in prompt output include relative times such as "3 hours ago".

## Scripts

Save expressions in `greet.purl`:

```purl
let names = ["Ada", "Bob"]
names | map(name => greeting.greet(name: name))
```

Run it with flags before the endpoint:

```sh
$ purl -f greet.purl 127.0.0.1:9000
$ purl -json -f greet.purl 127.0.0.1:9000
```

Scripts stop at the first error and print values without tables or a pager.
`-json` writes complete JSON, using `null` for nil and omitting colors and
relative timestamps.
