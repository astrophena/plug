# Purl and PlugScript

Purl reads a server's schema and calls its methods.
PlugScript is the expression language used at the prompt and in `.purl` files.

Install Purl as described in the [README](../README.md#tools).
With the server from [Using Plug](intro.md) running:

```sh
$ purl 127.0.0.1:9000
```

Enter a method name to read its documentation.
Add parentheses and named arguments to call it:

```purl
greeting.greet
greeting.greet(name: "Ada")
```

Tab completes methods and arguments without making calls.
Use `:help` to explore the API and builtins;
`purl -help` lists endpoints, flags, keyboard controls, paging, and history settings.
Raw methods are not supported.

After detecting a lost connection, Purl reconnects before the next call.
The server must still have the same schema.
A failed call is never retried automatically;
it may already have changed server state.

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

`null` is an alias for `nil`.
Lists use zero-based indexes.
Read record fields with a dot or string index:

```purl
let names = ["Ada", "Bob"]
names[0] // "Ada"
let person = {name: "Ada", active: true}
person.name // "Ada"
person["name"] // "Ada"
```

Arithmetic uses `+`, `-`, `*`, `/`, and `%`.
Compare values with `==`, `!=`, `<`, `<=`, `>`, and `>=`.
Boolean operators are `&&`, `||`, and `!`;
`&&` and `||` stop when the result is known.
Integers stay exact beyond machine integer sizes.
Comments use `//` or `/* ... */`.

## Bindings

`let` names an expression without evaluating it.
The first use evaluates it and saves the result:

```purl
let message = greeting.greet(name: "Ada")
message
message | count
```

Reading `message` makes one RPC call.
`message | count` counts the characters in the saved string.
Side effects happen on first use, and errors are saved too, including cancellation.
Rebind the expression to try again.

Rebinding a name does not change existing bindings:

```purl
let x = 1
let saved = x
let x = 2
saved // 1
```

`saved` uses `x` from its defining scope.

## Functions

Define functions with `=>`.
Each call evaluates the body:

```purl
let twice = x => x * 2
twice(3) // 6
let add = (a, b) => a + b
add(2, 3) // 5
```

Use a function when each use should make a fresh RPC call:

```purl
let refresh = () => greeting.greet(name: "Ada")
refresh()
refresh()
```

Functions retain their defining scope.
Recursion needs an explicit self argument:

```purl
let done = (self, n) => n == 0 || self(self, n - 1)
done(done, 1024)
```

Tail calls reuse the evaluator's stack and can exceed the usual 256-level evaluation limit.
Timeouts and Ctrl-C still stop evaluation.

## Pipelines and builtins

`x | f(y)` means `f(x, y)`.
Omit parentheses when there are no other arguments:

```purl
let rows = [{name: "Ada", size: 3}, {name: "Bob", size: 5}]
rows | filter(r => r.size > 3) | map(r => r.name)
rows | sortBy(r => r.name) | take(1)
rows | map(r => r.size) | sum
rows | count
```

The first pipeline returns `["Bob"]`.
Pipelines pass complete values;
they do not stream results or fetch more pages.

Arguments are evaluated before the function runs.
Use `:help NAME` for a builtin's arguments and behavior.

## Results

`_` holds the complete last successful result.
Save it before another successful expression replaces it:

```purl
rows
let result = _
result[0].name
```

The prompt displays lists of similarly shaped records as tables when they fit.
The `#` column is the list index.
Long cells may be shortened;
use `_[0]` or a saved result to read a full row.
Different record shapes or a narrow terminal produce ordinary records instead.

Long output uses a pager.
Without one, large results are shortened.
Timestamps include relative times such as "3 hours ago".
These display choices leave values unchanged and make no RPC calls.

## Scripts

Save this as `greet.purl`:

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
`-json` writes complete JSON, using `null` for nil and omitting colors and relative times.
