package clip

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// reset clears DefaultParser between tests.
func reset() {
	DefaultParser.Close()
	*DefaultParser = *New()
}

// ---- parseSize ---------------------------------------------------------------

func TestParseSizePlainInt(t *testing.T) {
	n, err := parseSize("1048576")
	if err != nil || n != 1048576 {
		t.Errorf("got %d, %v; want 1048576, nil", n, err)
	}
}

func TestParseSizeKilo(t *testing.T) {
	n, err := parseSize("10k")
	if err != nil || n != 10*1024 {
		t.Errorf("got %d, %v; want %d, nil", n, err, 10*1024)
	}
}

func TestParseSizeMega(t *testing.T) {
	n, err := parseSize("2M")
	if err != nil || n != 2*1024*1024 {
		t.Errorf("got %d, %v", n, err)
	}
}

func TestParseSizeGiga(t *testing.T) {
	n, err := parseSize("1G")
	if err != nil || n != 1*1024*1024*1024 {
		t.Errorf("got %d, %v", n, err)
	}
}

func TestParseSizeEmpty(t *testing.T) {
	n, err := parseSize("")
	if err != nil || n != 0 {
		t.Errorf("got %d, %v; want 0, nil", n, err)
	}
}

func TestParseSizeInvalid(t *testing.T) {
	if _, err := parseSize("abc"); err == nil {
		t.Error("expected error for invalid size")
	}
}

// ---- Parser / DefaultParser --------------------------------------------------

func TestNewIsIndependent(t *testing.T) {
	// Two parsers must not share any state.
	reset()
	defer reset()

	p1 := New()
	p2 := New()

	var v1, v2 string
	p1.ArgOption(&v1, 'f', "file", "F", "")
	p2.ArgOption(&v2, 'g', "get", "G", "")

	if _, err := p1.Parse([]string{"prog", "-f", "alpha"}); err != nil {
		t.Fatal(err)
	}
	defer p1.Close()

	if _, err := p2.Parse([]string{"prog", "-g", "beta"}); err != nil {
		t.Fatal(err)
	}
	defer p2.Close()

	if v1 != "alpha" || v2 != "beta" {
		t.Errorf("v1=%q v2=%q; want alpha/beta", v1, v2)
	}
	// p2 must not have seen p1's -f option
	if len(p2.opts) != 2 { // -g + help
		t.Errorf("p2 has %d opts; want 2", len(p2.opts))
	}
}

// ---- Args accumulation -------------------------------------------------------

func TestArgsResetBetweenParses(t *testing.T) {
	reset()
	var f1 string
	DefaultParser.ArgOption(&f1, 'f', "foo", "V", "")
	if _, err := Parse([]string{"prog", "-f", "first"}); err != nil {
		t.Fatal(err)
	}
	DefaultParser.Close()

	reset()
	var f2 string
	DefaultParser.ArgOption(&f2, 'f', "foo", "V", "")
	if _, err := Parse([]string{"prog", "-f", "second"}); err != nil {
		t.Fatal(err)
	}
	defer DefaultParser.Close()

	if len(DefaultParser.Args) != 3 {
		t.Errorf("Args = %v; want [prog -f second]", DefaultParser.Args)
	}
	if f2 != "second" {
		t.Errorf("f2 = %q; want \"second\"", f2)
	}
}

// ---- Goroutine leak ----------------------------------------------------------

func TestNoGoroutineLeakOnRepeatedParse(t *testing.T) {
	reset()
	before := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		reset()
		var f string
		DefaultParser.ArgOption(&f, 'f', "foo", "V", "")
		Parse([]string{"prog", "-f", "x"})
		DefaultParser.Close()
	}

	time.Sleep(20 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+1 {
		t.Errorf("goroutine count grew %d → %d; likely leak", before, after)
	}
}

func TestGoroutineCleanedUpByClose(t *testing.T) {
	reset()
	before := runtime.NumGoroutine()

	Parse([]string{"prog"})
	Close()

	time.Sleep(20 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before {
		t.Errorf("goroutine count after Close: %d (was %d); likely leak", after, before)
	}
}

// ---- ErrHelp (replaces os.Exit) ---------------------------------------------

func TestParseReturnsErrHelp(t *testing.T) {
	// ErrHelp must be returned instead of calling os.Exit(0).
	p := New()
	defer p.Close()

	_, err := p.Parse([]string{"prog", "--help"})
	if !errors.Is(err, ErrHelp) {
		t.Errorf("got %v; want ErrHelp", err)
	}
}

func TestParseReturnsErrHelpShort(t *testing.T) {
	p := New()
	defer p.Close()

	_, err := p.Parse([]string{"prog", "-h"})
	if !errors.Is(err, ErrHelp) {
		t.Errorf("got %v; want ErrHelp", err)
	}
}

// ---- helpOption dedup --------------------------------------------------------

func TestHelpOptionAddedOnce(t *testing.T) {
	reset()
	defer reset()

	Parse([]string{"prog"})
	Parse([]string{"prog"}) // second call on same parser state
	defer DefaultParser.Close()

	count := 0
	for _, o := range DefaultParser.opts {
		if o == &DefaultParser.helpOption {
			count++
		}
	}
	if count != 1 {
		t.Errorf("helpOption appears %d time(s); want 1", count)
	}
}

// ---- double-dash end-of-options ---------------------------------------------

func TestDoubleDashEndsOptions(t *testing.T) {
	reset()
	defer reset()

	var verbose bool
	DefaultParser.FlagOption(&verbose, 'v', "verbose", "")

	cmd, err := Parse([]string{"prog", "-v", "--", "--not-a-flag", "arg2"})
	if err != nil {
		t.Fatal(err)
	}
	defer DefaultParser.Close()

	if !verbose {
		t.Error("-v before -- should have been parsed")
	}
	if len(cmd.Arguments) != 2 || cmd.Arguments[0] != "--not-a-flag" {
		t.Errorf("Arguments = %v; want [--not-a-flag arg2]", cmd.Arguments)
	}
}

// ---- uint64 support ---------------------------------------------------------

func TestArgOptionUint64(t *testing.T) {
	reset()
	defer reset()

	var n uint64
	DefaultParser.ArgOption(&n, 0, "count", "N", "")

	if _, err := Parse([]string{"prog", "--count", "18446744073709551615"}); err != nil {
		t.Fatal(err)
	}
	defer DefaultParser.Close()

	if n != 18446744073709551615 {
		t.Errorf("n = %d; want max uint64", n)
	}
}

// ---- PositionalCustom guard --------------------------------------------------

func TestPositionalCustomPanicsWhenSubcmdsExist(t *testing.T) {
	reset()
	defer reset()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	DefaultParser.SubCommand("sub", "", "")
	var s string
	DefaultParser.PositionalCustom((*clipString)(&s), "arg", "")
}

// ---- SetLogBufSize -----------------------------------------------------------

func TestSetLogBufSize(t *testing.T) {
	p := New()
	p.SetLogBufSize(128)
	defer p.Close()

	if _, err := p.Parse([]string{"prog"}); err != nil {
		t.Fatal(err)
	}
	// Channel should accept 128 entries without blocking.
	for i := 0; i < 128; i++ {
		p.Logf("msg %d", i)
	}
	// If SetLogBufSize had no effect (buffer=64), sending 128 entries would
	// deadlock.  Reaching here means the buffer is at least 128.
}

// ---- Logging -----------------------------------------------------------------

func TestLogfPercentNotReinterpreted(t *testing.T) {
	reset()
	defer reset()

	f, err := os.CreateTemp("", "clip-test-*.log")
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	f.Close()
	defer os.Remove(name)

	if err := OpenLogfile(name, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse([]string{"prog"}); err != nil {
		t.Fatal(err)
	}

	DefaultParser.Logf("progress: %d%%", 75)
	DefaultParser.Logf("path: /tmp/%%s/file")
	DefaultParser.Close()

	data, _ := os.ReadFile(name)
	content := string(data)
	if strings.Contains(content, "%!(") {
		t.Errorf("log output contains format-verb artefacts:\n%s", content)
	}
	if !strings.Contains(content, "progress: 75%") {
		t.Errorf("expected \"progress: 75%%\" in log:\n%s", content)
	}
}

func TestLogfNoDuplicateNamePrefix(t *testing.T) {
	reset()
	defer reset()

	DefaultParser.Name = "myapp"

	f, err := os.CreateTemp("", "clip-test-*.log")
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	f.Close()
	defer os.Remove(name)

	if err := OpenLogfile(name, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse([]string{"prog"}); err != nil {
		t.Fatal(err)
	}

	DefaultParser.Logf("hello world")
	DefaultParser.Close()

	data, _ := os.ReadFile(name)
	if count := strings.Count(string(data), "[myapp]"); count != 1 {
		t.Errorf("[myapp] appears %d time(s); want 1:\n%s", count, string(data))
	}
}

func TestLogRotationMessageNotDropped(t *testing.T) {
	reset()
	defer reset()

	f, err := os.CreateTemp("", "clip-test-*.log")
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	defer os.Remove(name + ".0")

	// 1-byte limit: rotation fires after every write.  With the fix, the
	// message that triggers rotation is written before the rotate, so it always
	// ends up in .0.  The old code nil'd the logger first → silent drop.
	if err := OpenLogfile(name, "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse([]string{"prog"}); err != nil {
		t.Fatal(err)
	}

	DefaultParser.Logf("first")
	DefaultParser.Logf("second") // triggers rotation in old code → was dropped
	DefaultParser.Close()

	var all strings.Builder
	for _, path := range []string{name, name + ".0"} {
		if data, err := os.ReadFile(path); err == nil {
			all.Write(data)
		}
	}
	if !strings.Contains(all.String(), "second") {
		t.Errorf("rotation-triggering message dropped:\n%s", all.String())
	}
}

func TestLogToStdoutWhenNoPath(t *testing.T) {
	reset()
	defer reset()

	if _, err := Parse([]string{"prog"}); err != nil {
		t.Fatal(err)
	}
	DefaultParser.Logf("stdout log line")
	DefaultParser.Close()
}

// ---- Basic option parsing ---------------------------------------------------

func TestParseFlagShort(t *testing.T) {
	reset()
	defer reset()

	var v bool
	FlagOption(&v, 'v', "verbose", "")
	if _, err := Parse([]string{"prog", "-v"}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	if !v {
		t.Error("expected verbose=true")
	}
}

func TestParseFlagLong(t *testing.T) {
	reset()
	defer reset()

	var v bool
	FlagOption(&v, 'v', "verbose", "")
	if _, err := Parse([]string{"prog", "--verbose"}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	if !v {
		t.Error("expected verbose=true")
	}
}

func TestParseArgOptionLong(t *testing.T) {
	reset()
	defer reset()

	var out string
	ArgOption(&out, 'o', "output", "FILE", "")
	if _, err := Parse([]string{"prog", "--output", "file.txt"}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	if out != "file.txt" {
		t.Errorf("out = %q; want \"file.txt\"", out)
	}
}

func TestParseArgOptionLongEquals(t *testing.T) {
	reset()
	defer reset()

	var out string
	ArgOption(&out, 'o', "output", "FILE", "")
	if _, err := Parse([]string{"prog", "--output=file.txt"}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	if out != "file.txt" {
		t.Errorf("out = %q; want \"file.txt\"", out)
	}
}

func TestParseArgOptionShort(t *testing.T) {
	reset()
	defer reset()

	var out string
	ArgOption(&out, 'o', "output", "FILE", "")
	if _, err := Parse([]string{"prog", "-o", "file.txt"}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	if out != "file.txt" {
		t.Errorf("out = %q; want \"file.txt\"", out)
	}
}

func TestParseArgOptionShortCombined(t *testing.T) {
	reset()
	defer reset()

	var out string
	ArgOption(&out, 'o', "output", "FILE", "")
	if _, err := Parse([]string{"prog", "-ofile.txt"}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	if out != "file.txt" {
		t.Errorf("out = %q; want \"file.txt\"", out)
	}
}

func TestParsePositional(t *testing.T) {
	reset()
	defer reset()

	var name string
	Positional(&name, "name", "")
	if _, err := Parse([]string{"prog", "Alice"}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	if name != "Alice" {
		t.Errorf("name = %q; want \"Alice\"", name)
	}
}

func TestParseMultiplePositionals(t *testing.T) {
	reset()
	defer reset()

	var a, b string
	Positional(&a, "first", "")
	Positional(&b, "second", "")
	if _, err := Parse([]string{"prog", "hello", "world"}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	if a != "hello" || b != "world" {
		t.Errorf("a=%q b=%q; want hello/world", a, b)
	}
}

func TestParseIncrOption(t *testing.T) {
	reset()
	defer reset()

	var level int
	IncrOption(&level, 'v', "verbose", "")
	if _, err := Parse([]string{"prog", "-v", "-v", "-v"}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	if level != 3 {
		t.Errorf("level = %d; want 3", level)
	}
}

func TestParseRemainingArguments(t *testing.T) {
	reset()
	defer reset()

	var f string
	ArgOption(&f, 'f', "file", "F", "")
	cmd, err := Parse([]string{"prog", "-f", "x", "extra1", "extra2"})
	if err != nil {
		t.Fatal(err)
	}
	defer Close()
	if len(cmd.Arguments) != 2 || cmd.Arguments[0] != "extra1" {
		t.Errorf("Arguments = %v; want [extra1 extra2]", cmd.Arguments)
	}
}

// ---- Sub-commands -----------------------------------------------------------

func TestParseSubCommand(t *testing.T) {
	reset()
	defer reset()

	called := false
	sub := SubCommand("serve", "", "")
	sub.SetRuns(func(c *Command) error { called = true; return nil }, nil, nil)

	cmd, err := Parse([]string{"prog", "serve"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("serve handler was not called")
	}
}

func TestParseSubCommandPrefix(t *testing.T) {
	reset()
	defer reset()

	sub := SubCommand("serve", "", "")
	sub.SetRuns(func(c *Command) error { return nil }, nil, nil)

	cmd, err := Parse([]string{"prog", "ser"})
	if err != nil {
		t.Fatal(err)
	}
	defer Close()
	if cmd.Name != "serve" {
		t.Errorf("matched %q; want \"serve\"", cmd.Name)
	}
}

func TestParseSubCommandAmbiguous(t *testing.T) {
	reset()
	defer reset()

	SubCommand("serve", "", "")
	SubCommand("search", "", "")
	_, err := Parse([]string{"prog", "se"})
	if err == nil {
		t.Error("expected ambiguous-command error")
	}
	Close()
}

func TestParseSubCommandExactMatchOverPrefix(t *testing.T) {
	reset()
	defer reset()

	SubCommand("gcloud", "", "")
	SubCommand("gcloud-new", "", "")

	cmd, err := Parse([]string{"prog", "gcloud"})
	if err != nil {
		t.Fatal(err)
	}
	defer Close()
	if cmd.Name != "gcloud" {
		t.Errorf("matched %q; want \"gcloud\"", cmd.Name)
	}
}

func TestParseSubCommandPrefixOfLongerName(t *testing.T) {
	reset()
	defer reset()

	SubCommand("gcloud", "", "")
	SubCommand("gcloud-new", "", "")

	cmd, err := Parse([]string{"prog", "gcloud-n"})
	if err != nil {
		t.Fatal(err)
	}
	defer Close()
	if cmd.Name != "gcloud-new" {
		t.Errorf("matched %q; want \"gcloud-new\"", cmd.Name)
	}
}

func TestParseSubCommandWithOption(t *testing.T) {
	reset()
	defer reset()

	var port int
	sub := SubCommand("serve", "", "")
	sub.ArgOption(&port, 'p', "port", "PORT", "")
	sub.SetRuns(func(c *Command) error { return nil }, nil, nil)

	cmd, err := Parse([]string{"prog", "serve", "--port", "8080"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if port != 8080 {
		t.Errorf("port = %d; want 8080", port)
	}
}

// ---- Error cases ------------------------------------------------------------

func TestParseUnknownFlag(t *testing.T) {
	reset()
	defer reset()
	_, err := Parse([]string{"prog", "--no-such-flag"})
	if err == nil {
		t.Error("expected error for unknown flag")
	}
	Close()
}

func TestParseMustSet(t *testing.T) {
	reset()
	defer reset()
	var val string
	ArgOption(&val, 'f', "file", "FILE", "").MustSet()
	_, err := Parse([]string{"prog"})
	if err == nil {
		t.Error("expected must-set error")
	}
	Close()
}

func TestParseDuplicateOption(t *testing.T) {
	reset()
	defer reset()
	var val string
	ArgOption(&val, 'f', "file", "FILE", "")
	_, err := Parse([]string{"prog", "-f", "a", "-f", "b"})
	if err == nil {
		t.Error("expected duplicate-option error")
	}
	Close()
}

func TestParseRepeatableOption(t *testing.T) {
	reset()
	defer reset()
	var val string
	ArgOption(&val, 'f', "file", "FILE", "").Repeatable(true)
	if _, err := Parse([]string{"prog", "-f", "a", "-f", "b"}); err != nil {
		t.Fatalf("repeatable option should not error: %v", err)
	}
	defer Close()
	if val != "b" {
		t.Errorf("val = %q; want \"b\"", val)
	}
}

func TestParseReverseFlag(t *testing.T) {
	reset()
	defer reset()
	v := true
	FlagOption(&v, 'n', "no-verbose", "").ReverseFlag()
	if _, err := Parse([]string{"prog", "-n"}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	if v {
		t.Error("reverse flag should have set v to false")
	}
}

// ---- Mutual exclusion panics ------------------------------------------------

func TestPositionalPanicsWhenSubcmdsExist(t *testing.T) {
	reset()
	defer reset()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	SubCommand("sub", "", "")
	var s string
	Positional(&s, "arg", "")
}

func TestSubCommandPanicsWhenPositionalsExist(t *testing.T) {
	reset()
	defer reset()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	var s string
	Positional(&s, "arg", "")
	SubCommand("sub", "", "")
}

// ---- Run lifecycle ----------------------------------------------------------

func TestRunCallsInitAndFini(t *testing.T) {
	reset()
	defer reset()

	var order []string
	SetRuns(
		func(c *Command) error { order = append(order, "run"); return nil },
		func(c *Command) error { order = append(order, "init"); return nil },
		func(c *Command) error { order = append(order, "fini"); return nil },
	)
	cmd, err := Parse([]string{"prog"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	want := []string{"init", "run", "fini"}
	for i, s := range want {
		if i >= len(order) || order[i] != s {
			t.Fatalf("order = %v; want %v", order, want)
		}
	}
}

func TestRunNotRunnableError(t *testing.T) {
	reset()
	defer reset()
	cmd, err := Parse([]string{"prog"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != ErrNotRunnable {
		t.Errorf("got %v; want ErrNotRunnable", err)
	}
}
