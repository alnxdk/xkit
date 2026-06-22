package clip

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// reset clears all package-level state between tests.  It stops any running
// logging goroutine before zeroing RootCmd so no goroutine is left dangling.
func reset() {
	RootCmd.closeLogfile()
	RootCmd = Command{}
	Args = nil
	helpOption = Option{shortName: 'h', longName: "help", desc: "Help information"}
	progInfo = ""
}

// ---- parseSize ----------------------------------------------------------------

func TestParseSizePlainInt(t *testing.T) {
	// Regression: factor==0 (no suffix) previously caused n*=0, returning 0.
	n, err := parseSize("1048576")
	if err != nil || n != 1048576 {
		t.Errorf("parseSize(\"1048576\") = %d, %v; want 1048576, nil", n, err)
	}
}

func TestParseSizeKilo(t *testing.T) {
	n, err := parseSize("10k")
	if err != nil || n != 10*1024 {
		t.Errorf("parseSize(\"10k\") = %d, %v; want %d, nil", n, err, 10*1024)
	}
}

func TestParseSizeKiloUpper(t *testing.T) {
	n, err := parseSize("10K")
	if err != nil || n != 10*1024 {
		t.Errorf("parseSize(\"10K\") = %d, %v; want %d, nil", n, err, 10*1024)
	}
}

func TestParseSizeMega(t *testing.T) {
	n, err := parseSize("2M")
	if err != nil || n != 2*1024*1024 {
		t.Errorf("parseSize(\"2M\") = %d, %v", n, err)
	}
}

func TestParseSizeGiga(t *testing.T) {
	n, err := parseSize("1G")
	if err != nil || n != 1*1024*1024*1024 {
		t.Errorf("parseSize(\"1G\") = %d, %v", n, err)
	}
}

func TestParseSizeEmpty(t *testing.T) {
	// Empty string means "no limit"; returns 0 without error.
	n, err := parseSize("")
	if err != nil || n != 0 {
		t.Errorf("parseSize(\"\") = %d, %v; want 0, nil", n, err)
	}
}

func TestParseSizeInvalid(t *testing.T) {
	if _, err := parseSize("abc"); err == nil {
		t.Error("parseSize(\"abc\") should return an error")
	}
}

func TestParseSizeHex(t *testing.T) {
	// strconv.ParseInt with base 0 accepts 0x-prefixed hex.
	n, err := parseSize("0x400")
	if err != nil || n != 1024 {
		t.Errorf("parseSize(\"0x400\") = %d, %v; want 1024, nil", n, err)
	}
}

// ---- Args accumulation --------------------------------------------------------

func TestArgsResetBetweenParses(t *testing.T) {
	// Regression: Args was never cleared, so second Parse saw stale entries.
	reset()
	var f1 string
	RootCmd.ArgOption(&f1, 'f', "foo", "V", "")
	if _, err := Parse([]string{"prog", "-f", "first"}); err != nil {
		t.Fatal(err)
	}
	RootCmd.closeLogfile()

	reset()
	var f2 string
	RootCmd.ArgOption(&f2, 'f', "foo", "V", "")
	if _, err := Parse([]string{"prog", "-f", "second"}); err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if len(Args) != 3 {
		t.Errorf("Args = %v (len=%d); want [prog -f second]", Args, len(Args))
	}
	if f2 != "second" {
		t.Errorf("f2 = %q; want \"second\"", f2)
	}
}

func TestArgsSkipsEmptyStrings(t *testing.T) {
	reset()
	defer func() { reset() }()

	Parse([]string{"prog", "", "hello", ""})
	RootCmd.closeLogfile()

	for _, a := range Args {
		if a == "" {
			t.Errorf("Args contains empty string: %v", Args)
		}
	}
}

// ---- Goroutine leak -----------------------------------------------------------

func TestNoGoroutineLeakOnRepeatedParse(t *testing.T) {
	// Regression: each Parse() used to unconditionally start a new goroutine,
	// leaking the previous one when Parse was called without an intervening Run.
	reset()
	before := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		reset()
		var f string
		RootCmd.ArgOption(&f, 'f', "foo", "V", "")
		Parse([]string{"prog", "-f", "x"})
		RootCmd.closeLogfile() // simulates what Run() does
	}

	time.Sleep(20 * time.Millisecond) // let goroutines exit
	after := runtime.NumGoroutine()
	if after > before+1 {
		t.Errorf("goroutine count grew from %d to %d; likely leak", before, after)
	}
}

func TestGoroutineCleanedUpByClose(t *testing.T) {
	reset()
	before := runtime.NumGoroutine()

	Parse([]string{"prog"})
	Close() // explicit cleanup without calling Run

	time.Sleep(20 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before {
		t.Errorf("goroutine count after Close: %d (was %d); likely leak", after, before)
	}
}

// ---- helpOption dedup ---------------------------------------------------------

func TestHelpOptionAddedOnce(t *testing.T) {
	// Regression: each Parse() appended &helpOption without checking for dups.
	reset()
	Parse([]string{"prog"})
	// Do NOT closeLogfile yet — call Parse again without full reset to verify
	// the dedup guard.
	Parse([]string{"prog"})
	defer RootCmd.closeLogfile()

	count := 0
	for _, o := range RootCmd.opts {
		if o == &helpOption {
			count++
		}
	}
	if count != 1 {
		t.Errorf("&helpOption appears %d time(s) in RootCmd.opts; want exactly 1", count)
	}
}

// ---- PositionalCustom guard ---------------------------------------------------

func TestPositionalCustomPanicsWhenSubcmdsExist(t *testing.T) {
	// Regression: PositionalCustom lacked the subcmds guard that Positional has.
	reset()
	defer reset()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when calling PositionalCustom after SubCommand")
		}
	}()

	RootCmd.SubCommand("sub", "a subcommand", "")
	var s string
	RootCmd.PositionalCustom((*clipString)(&s), "arg", "should panic")
}

func TestPositionalPanicsWhenSubcmdsExist(t *testing.T) {
	reset()
	defer reset()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()

	RootCmd.SubCommand("sub", "a subcommand", "")
	var s string
	RootCmd.Positional(&s, "arg", "should panic")
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
	RootCmd.Positional(&s, "arg", "")
	RootCmd.SubCommand("sub", "should panic", "")
}

// ---- Logging ------------------------------------------------------------------

func TestLogfPercentNotReinterpreted(t *testing.T) {
	// Regression: logger.Printf(s) re-interpreted '%' in the already-formatted
	// string; the fix is logger.Printf("%s", s).
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

	RootCmd.Logf("progress: %d%%", 75)      // formatted string will contain "75%"
	RootCmd.Logf("path: /tmp/%%s/file.txt") // literal %s in message
	RootCmd.closeLogfile()

	data, _ := os.ReadFile(name)
	content := string(data)
	if strings.Contains(content, "%!(") {
		t.Errorf("log output contains format verb artefacts:\n%s", content)
	}
	if !strings.Contains(content, "progress: 75%") {
		t.Errorf("log output missing expected content:\n%s", content)
	}
}

func TestLogfNoDuplicateNamePrefix(t *testing.T) {
	// Regression: Logf prepended [Name] AND the logger also had [Name] as its
	// prefix, producing "[Name] DATETIME [Name] message".
	reset()
	defer reset()

	RootCmd.Name = "myapp"

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

	RootCmd.Logf("hello world")
	RootCmd.closeLogfile()

	data, _ := os.ReadFile(name)
	content := string(data)
	count := strings.Count(content, "[myapp]")
	if count != 1 {
		t.Errorf("[myapp] appears %d time(s) per log line; want 1:\n%s", count, content)
	}
}

func TestLogRotationMessageNotDropped(t *testing.T) {
	// Regression: the message that triggered rotation was silently discarded
	// because the old code nil'd the logger (rotation check) before writing.
	//
	// Scenario with a 1-byte limit:
	//   OLD code — "first" written (file empty → no rotation check fires yet).
	//              "second" arrives: rotation fires, logger nil'd, then write is
	//              skipped → "second" is permanently lost.
	//   NEW code — both messages are written before their rotation checks, so
	//              "second" is always present in the .0 backup file.
	//
	// Note: with limit=1, every write triggers a rotation that overwrites .0,
	// so only the last-written message survives in .0; we assert only "second".
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

	if err := OpenLogfile(name, "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse([]string{"prog"}); err != nil {
		t.Fatal(err)
	}

	RootCmd.Logf("first")
	RootCmd.Logf("second") // this triggers rotation; old code dropped it
	RootCmd.closeLogfile()

	var all strings.Builder
	for _, path := range []string{name, name + ".0"} {
		if data, err := os.ReadFile(path); err == nil {
			all.Write(data)
		}
	}
	if !strings.Contains(all.String(), "second") {
		t.Errorf("rotation-triggering message was dropped:\n%s", all.String())
	}
}

func TestLogToStdoutWhenNoPath(t *testing.T) {
	// When no logfile path is set, the goroutine should assign os.Stdout and
	// not panic or error.
	reset()
	defer reset()

	if _, err := Parse([]string{"prog"}); err != nil {
		t.Fatal(err)
	}
	// Logf should not block or panic.
	RootCmd.Logf("stdout log line")
	RootCmd.closeLogfile()
}

// ---- Basic option parsing -----------------------------------------------------

func TestParseFlagShort(t *testing.T) {
	reset()
	defer reset()

	var v bool
	RootCmd.FlagOption(&v, 'v', "verbose", "")
	if _, err := Parse([]string{"prog", "-v"}); err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if !v {
		t.Error("verbose should be true after -v")
	}
}

func TestParseFlagLong(t *testing.T) {
	reset()
	defer reset()

	var v bool
	RootCmd.FlagOption(&v, 'v', "verbose", "")
	if _, err := Parse([]string{"prog", "--verbose"}); err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if !v {
		t.Error("verbose should be true after --verbose")
	}
}

func TestParseArgOptionLong(t *testing.T) {
	reset()
	defer reset()

	var out string
	RootCmd.ArgOption(&out, 'o', "output", "FILE", "")
	if _, err := Parse([]string{"prog", "--output", "file.txt"}); err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if out != "file.txt" {
		t.Errorf("out = %q; want \"file.txt\"", out)
	}
}

func TestParseArgOptionLongEquals(t *testing.T) {
	reset()
	defer reset()

	var out string
	RootCmd.ArgOption(&out, 'o', "output", "FILE", "")
	if _, err := Parse([]string{"prog", "--output=file.txt"}); err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if out != "file.txt" {
		t.Errorf("out = %q; want \"file.txt\"", out)
	}
}

func TestParseArgOptionShort(t *testing.T) {
	reset()
	defer reset()

	var out string
	RootCmd.ArgOption(&out, 'o', "output", "FILE", "")
	if _, err := Parse([]string{"prog", "-o", "file.txt"}); err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if out != "file.txt" {
		t.Errorf("out = %q; want \"file.txt\"", out)
	}
}

func TestParseArgOptionShortCombined(t *testing.T) {
	reset()
	defer reset()

	var out string
	RootCmd.ArgOption(&out, 'o', "output", "FILE", "")
	if _, err := Parse([]string{"prog", "-ofile.txt"}); err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if out != "file.txt" {
		t.Errorf("out = %q; want \"file.txt\"", out)
	}
}

func TestParsePositional(t *testing.T) {
	reset()
	defer reset()

	var name string
	RootCmd.Positional(&name, "name", "")
	if _, err := Parse([]string{"prog", "Alice"}); err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if name != "Alice" {
		t.Errorf("name = %q; want \"Alice\"", name)
	}
}

func TestParseMultiplePositionals(t *testing.T) {
	reset()
	defer reset()

	var a, b string
	RootCmd.Positional(&a, "first", "")
	RootCmd.Positional(&b, "second", "")
	if _, err := Parse([]string{"prog", "hello", "world"}); err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if a != "hello" || b != "world" {
		t.Errorf("a=%q b=%q; want \"hello\" \"world\"", a, b)
	}
}

func TestParseIncrOption(t *testing.T) {
	reset()
	defer reset()

	var level int
	RootCmd.IncrOption(&level, 'v', "verbose", "")
	if _, err := Parse([]string{"prog", "-v", "-v", "-v"}); err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if level != 3 {
		t.Errorf("level = %d; want 3", level)
	}
}

func TestParseRemainingArguments(t *testing.T) {
	reset()
	defer reset()

	var f string
	RootCmd.ArgOption(&f, 'f', "file", "F", "")
	cmd, err := Parse([]string{"prog", "-f", "x", "extra1", "extra2"})
	if err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if len(cmd.Arguments) != 2 || cmd.Arguments[0] != "extra1" || cmd.Arguments[1] != "extra2" {
		t.Errorf("Arguments = %v; want [extra1 extra2]", cmd.Arguments)
	}
}

// ---- Sub-commands -------------------------------------------------------------

func TestParseSubCommand(t *testing.T) {
	reset()
	defer reset()

	called := false
	sub := RootCmd.SubCommand("serve", "start server", "")
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

	sub := RootCmd.SubCommand("serve", "", "")
	sub.SetRuns(func(c *Command) error { return nil }, nil, nil)

	cmd, err := Parse([]string{"prog", "ser"})
	if err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if cmd.Name != "serve" {
		t.Errorf("matched command %q; want \"serve\"", cmd.Name)
	}
}

func TestParseSubCommandAmbiguous(t *testing.T) {
	reset()
	defer reset()

	RootCmd.SubCommand("serve", "", "")
	RootCmd.SubCommand("search", "", "")

	_, err := Parse([]string{"prog", "se"})
	if err == nil {
		t.Error("expected ambiguous-command error")
	}
	RootCmd.closeLogfile()
}

func TestParseSubCommandUnknown(t *testing.T) {
	reset()
	defer reset()

	RootCmd.SubCommand("serve", "", "")

	_, err := Parse([]string{"prog", "unknown"})
	if err == nil {
		t.Error("expected error for unknown sub-command")
	}
	RootCmd.closeLogfile()
}

func TestParseSubCommandWithOption(t *testing.T) {
	reset()
	defer reset()

	var port int
	sub := RootCmd.SubCommand("serve", "", "")
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

// ---- Error cases --------------------------------------------------------------

func TestParseUnknownFlag(t *testing.T) {
	reset()
	defer reset()

	_, err := Parse([]string{"prog", "--no-such-flag"})
	if err == nil {
		t.Error("expected error for unknown flag")
	}
	RootCmd.closeLogfile()
}

func TestParseMustSet(t *testing.T) {
	reset()
	defer reset()

	var val string
	RootCmd.ArgOption(&val, 'f', "file", "FILE", "").MustSet()

	_, err := Parse([]string{"prog"})
	if err == nil {
		t.Error("expected error: must-set option not provided")
	}
	RootCmd.closeLogfile()
}

func TestParseDuplicateOption(t *testing.T) {
	reset()
	defer reset()

	var val string
	RootCmd.ArgOption(&val, 'f', "file", "FILE", "")

	_, err := Parse([]string{"prog", "-f", "a", "-f", "b"})
	if err == nil {
		t.Error("expected error: option set more than once")
	}
	RootCmd.closeLogfile()
}

func TestParseRepeatableOption(t *testing.T) {
	reset()
	defer reset()

	var val string
	RootCmd.ArgOption(&val, 'f', "file", "FILE", "").Repeatable(true)

	_, err := Parse([]string{"prog", "-f", "a", "-f", "b"})
	if err != nil {
		t.Fatalf("repeatable option should not error: %v", err)
	}
	defer RootCmd.closeLogfile()

	if val != "b" {
		t.Errorf("val = %q; want \"b\"", val)
	}
}

func TestParseReverseFlag(t *testing.T) {
	reset()
	defer reset()

	v := true
	RootCmd.FlagOption(&v, 'n', "no-verbose", "").ReverseFlag()

	if _, err := Parse([]string{"prog", "-n"}); err != nil {
		t.Fatal(err)
	}
	defer RootCmd.closeLogfile()

	if v {
		t.Error("reverse flag should have set v to false")
	}
}

// ---- Run ----------------------------------------------------------------------

func TestRunCallsInitAndFini(t *testing.T) {
	reset()
	defer reset()

	var order []string
	RootCmd.SetRuns(
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
	if len(order) != len(want) {
		t.Fatalf("call order = %v; want %v", order, want)
	}
	for i, s := range want {
		if order[i] != s {
			t.Errorf("order[%d] = %q; want %q", i, order[i], s)
		}
	}
}

func TestRunNotRunnableError(t *testing.T) {
	reset()
	defer reset()

	// A command with no run function returns ErrNotRunnable.
	cmd, err := Parse([]string{"prog"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != ErrNotRunnable {
		t.Errorf("Run() = %v; want ErrNotRunnable", err)
	}
}
