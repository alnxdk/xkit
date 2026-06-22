# Changelog

## v1.0.0

### Summary

This release introduces the `Parser` type so that multiple independent parsers
can coexist in the same process, fixes several correctness bugs, and adds three
new features (`--` end-of-options, `uint64` support, configurable log buffer).

---

### Breaking changes

#### 1. `clip.RootCmd` removed

`var RootCmd Command` no longer exists at the package level.

| Before | After |
|--------|-------|
| `clip.RootCmd.Name = "app"` | `clip.DefaultParser.Name = "app"` |
| `clip.RootCmd.Logf("msg")` | `clip.DefaultParser.Logf("msg")` |
| `clip.RootCmd.Arguments` | `cmd.Arguments` (returned by Parse) |

If you only use the package-level convenience functions (`clip.FlagOption`,
`clip.SubCommand`, `clip.Parse`, …) you do not need to change anything — they
all delegate to `clip.DefaultParser` exactly as before.

#### 2. `clip.Args` removed

`var Args []string` no longer exists at the package level.

| Before | After |
|--------|-------|
| `clip.Args` | `clip.DefaultParser.Args` |

#### 3. `clip.Parse` no longer calls `os.Exit(0)` for `--help` / `-h`

Previously `Parse` called `os.Exit(0)` when the user passed a help flag.
It now returns `(nil, clip.ErrHelp)` instead, giving the caller control over
the process lifecycle.

**Migrate every call site that follows Parse:**

```go
// Before
cmd, err := clip.Parse(nil)
if err != nil {
    fmt.Fprintln(os.Stderr, err)
    os.Exit(1)
}

// After
cmd, err := clip.Parse(nil)
if errors.Is(err, clip.ErrHelp) {
    os.Exit(0)          // help was already printed to stdout
}
if err != nil {
    fmt.Fprintln(os.Stderr, err)
    clip.Close()        // stop the logging goroutine before exiting
    os.Exit(1)
}
```

The help text is still printed automatically before `ErrHelp` is returned; you
only need to exit with the right status code.

---

### New features

#### `Parser` type and `New()` constructor

Programs that need more than one independent parser (or want to avoid global
state in tests) can now create isolated parsers:

```go
p := clip.New()
p.ProgDescription("my-tool")
p.FlagOption(&verbose, 'v', "verbose", "Enable verbose output")
sub := p.SubCommand("serve", "Start server", "")
sub.SetRuns(serveRun, nil, nil)

cmd, err := p.Parse(nil)
if errors.Is(err, clip.ErrHelp) { os.Exit(0) }
if err != nil { p.Close(); os.Exit(1) }
os.Exit(func() int {
    if err := cmd.Run(); err != nil { return 1 }
    return 0
}())
```

`Parser` embeds `Command`, so all `Command` registration methods (`FlagOption`,
`SubCommand`, `Positional`, `SetRuns`, `Logf`, …) are available directly on
`*Parser`.

#### `clip.ErrHelp` sentinel

```go
var ErrHelp = errors.New("help requested")
```

Returned by `Parse` (and `Parser.Parse`) when the user requests help.  The help
text has already been written to stdout; callers should `os.Exit(0)`.

#### `Parser.SetLogBufSize(n int) *Parser`

Sets the capacity of the internal log channel (default: 64).  Must be called
before `Parse`.

```go
p := clip.New()
p.SetLogBufSize(256)
```

#### `--` end-of-options

A bare `--` token now stops option processing.  All subsequent tokens — even
those starting with `-` — are placed verbatim in `Command.Arguments`.

```
prog -v -- --not-a-flag positional
```

#### `*uint64` supported in `ArgOption` / `Positional`

Passing a `*uint64` to `ArgOption` or `Positional` now works correctly.
Previously it panicked with `"use _Custom() for Option type *uint64"`.

---

### Bug fixes (already in v1 patch releases or first included here)

| # | Description |
|---|-------------|
| 1 | `parseSize` with no unit suffix (e.g. `"1048576"`) returned 0 instead of the value — log rotation was silently disabled. |
| 2 | `Parser.Args` (formerly `clip.Args`) was never reset between `Parse` calls, causing stale entries to accumulate. |
| 3 | The logging goroutine was started unconditionally on every `Parse` call, leaking goroutines when `Parse` was called without a subsequent `Run`. |
| 4 | `logger.Printf(s)` re-interpreted `%` characters in already-formatted log strings, producing garbled output. |
| 5 | `PositionalCustom` lacked the sub-command mutual-exclusion guard that `Positional` enforces. |
| 6 | `--help` was appended to `opts` on every `Parse` call with no deduplication. |
| 7 | The log message that triggered file rotation was silently dropped. |
| 8 | `Logf` added `[Name]` to the format string and the `log.Logger` also had `[Name]` as its prefix, resulting in a double prefix in every log line. |
