# Setting: struct

## struct:comment COMMENT

`struct:comment COMMENT` can be defined as [CLI argument](./define-settings.md#cli)
or [conversion comment](./define-settings.md#conversion).

`struct:comment` instructs goverter to add a comment line to the generated
struct. It can be configured multiple times to add multiline comments. Prefix
your COMMENT with `//` to force single line comment style.

::: code-group
<<< @../../example/struct-comment/input.go
<<< @../../example/struct-comment/generated/generated.go [generated/generated.go]
:::

## struct:assign STRATEGY

`struct:assign STRATEGY` can be defined as [CLI argument](./define-settings.md#cli),
[conversion comment](./define-settings.md#conversion) or
[method comment](./define-settings.md#method) and is
[inheritable](./define-settings.md#inheritance).

`struct:assign` controls which members of the target struct goverter tries to
satisfy from the source. `STRATEGY` is a space separated combination of:

- `field` (default): assign the exported struct fields, as goverter always has.
- `method`: assign through setter methods (see [`struct:assign:setter`](#struct-assign-setter-regex-template)).

Combine them (`field method`) to assign both. Assigning through setter methods is
useful for types that hide their fields behind methods, like structs generated for
the [gRPC/protobuf Opaque API](https://go.dev/blog/protobuf-opaque).

Because this setting is [inheritable](./define-settings.md#inheritance), defining it
on the converter interface (or via the `-g` CLI flag) applies it to every method
**and** every nested conversion goverter generates — so a deeply nested type tree
(like a large protobuf message) only needs a single declaration:

```go
// goverter:converter
// goverter:struct:assign method
type Converter interface {
    FromProto(source *pb.DeepMessage) *DeepMessage
}
```

::: code-group
<<< @../../example/struct-assign-setter/input.go
<<< @../../example/struct-assign-setter/generated/generated.go [generated/generated.go]
:::

A setter is a method with exactly one parameter that returns nothing or a single
`error`. When it returns an `error`, the conversion method must also return an
`error` and goverter forwards it. Methods that don't match the setter pattern (see
below) are ignored, so getters and other helper methods on the target don't
interfere.

[`goverter:map`](./map.md) and [`goverter:ignore`](./ignore.md) address a setter
by its method name, exactly as they address a field by its field name — e.g.
`goverter:map FullName SetName` or `goverter:ignore SetName`. When `field method`
is enabled and a field and a setter cover the same value, a `map`/`ignore` naming
one of them suppresses the other, so the referenced member wins.

## struct:assign:setter REGEX [TEMPLATE]

`struct:assign:setter REGEX [TEMPLATE]` can be defined as
[CLI argument](./define-settings.md#cli),
[conversion comment](./define-settings.md#conversion) or
[method comment](./define-settings.md#method) and is
[inheritable](./define-settings.md#inheritance).

`struct:assign:setter` configures how setter methods are recognized when
`struct:assign method` is enabled. `REGEX` must contain at least one capture group
and is matched against the full method name; methods that don't match are not
treated as setters. `TEMPLATE` (default `$1`) expands the captured groups into the
**output field name** the setter covers — so `SetName` covers the field `Name`,
which is then resolved against the source like any other field.

The default is equivalent to:

```go
// goverter:struct:assign:setter Set(.*) $1
```

To match builder-style methods like `WithName(name)` instead:

```go
// goverter:converter
// goverter:struct:assign method
// goverter:struct:assign:setter With(.*)
type Converter interface {
    Convert(source Input) Output
}
```

If the source exposes the value through a prefixed getter (e.g. `GetName()`) rather
than a plain field, normalize the source side with
[`struct:assign:source`](#struct-assign-source-regex-template) — the two settings
are mirrors: the setter regex names the output field, the source regex names the
source field.

## struct:assign:source REGEX [TEMPLATE]

`struct:assign:source REGEX [TEMPLATE]` can be defined as
[CLI argument](./define-settings.md#cli),
[conversion comment](./define-settings.md#conversion) or
[method comment](./define-settings.md#method) and is
[inheritable](./define-settings.md#inheritance).

`struct:assign:source` is the source-side mirror of
[`struct:assign:setter`](#struct-assign-setter-regex-template): it recognizes
source **accessor methods** and normalizes their names to the field name they
provide, so they participate in matching just like a plain source field. `REGEX`
must contain at least one capture group and is matched against the full method
name; `TEMPLATE` (default `$1`) expands the captures into the source field name.
It is generic — any naming convention works, getters being the common case. An
exact source field or method name always wins over a normalized accessor.

For prefixed getters like `GetName()`:

```go
// goverter:converter
// goverter:struct:assign method
// goverter:struct:assign:source Get(.*)
type Converter interface {
    Convert(source Input) Output
}

type Input struct{ name string }
func (i Input) GetName() string { return i.name }

type Output struct{ name string }
func (o *Output) SetName(name string) { o.name = name }
```

This generates `out.SetName(source.GetName())`. Because it only affects how source
values are found, it works regardless of whether the target member is a field or a
setter — a target field `Name` would likewise be filled from `source.GetName()`.

## struct:assign:prefer KIND

`struct:assign:prefer KIND` can be defined as [CLI argument](./define-settings.md#cli),
[conversion comment](./define-settings.md#conversion) or
[method comment](./define-settings.md#method) and is
[inheritable](./define-settings.md#inheritance).

When `struct:assign field method` is used and a field and a setter map from the same
source, goverter reports an error by default. `struct:assign:prefer` resolves this
automatically by choosing which one wins: `field` or `method`. An explicit
[`goverter:map`](./map.md) or [`goverter:ignore`](./ignore.md) always takes
precedence over this preference.

```go
// goverter:converter
// goverter:struct:assign field method
// goverter:struct:assign:prefer method
type Converter interface {
    Convert(source Input) Output
}
```
