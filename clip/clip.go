// Package clip is a compact command-line argument parser for Go programs.
//
// # Overview
//
// clip supports four kinds of command-line tokens:
//
//   - Flag options  (-v, --verbose)         toggle a bool value
//   - Argument options (-o f, --out=f)      consume the next token as a typed value
//   - Positional arguments                  non-flag tokens consumed in declaration order
//   - Sub-commands                          verb tokens that select a child [Command]
//
// A [Command] cannot mix positionals and sub-commands; adding both panics at
// setup time so the bug is caught before any argument is parsed.
//
// # Typical use — single parser (recommended)
//
//	p := clip.New()
//	p.ProgDescription("my-tool — does something useful")
//	p.FlagOption(&verbose, 'v', "verbose", "Enable verbose output")
//	sub := p.SubCommand("serve", "Start server", "")
//	sub.SetRuns(serveRun, nil, nil)
//
//	cmd, err := p.Parse(nil) // nil → os.Args
//	if errors.Is(err, clip.ErrHelp) {
//	    os.Exit(0)
//	}
//	if err != nil {
//	    fmt.Fprintln(os.Stderr, err)
//	    p.Close()
//	    os.Exit(1)
//	}
//	os.Exit(func() int {
//	    if err := cmd.Run(); err != nil { fmt.Fprintln(os.Stderr, err); return 1 }
//	    return 0
//	}())
//
// # Typical use — package-level convenience API (backwards-compatible)
//
// The package-level functions (FlagOption, SubCommand, Parse, …) delegate to
// [DefaultParser] and behave identically to calling the same methods on a
// Parser created with [New].  Use them when one global parser is enough.
//
// # Logging
//
// Call [Parser.OpenLogfile] (or package-level [OpenLogfile]) before [Parser.Parse]
// to direct log output to a file with optional size-based rotation.  If no path
// is configured, log lines are written to stdout.  Use [Command.Logf] inside
// run/init functions.  All I/O is serialised through a single goroutine.
//
// [Command.Run] closes the logging goroutine automatically.  If you exit before
// calling Run (e.g. after a [Parse] error), call [Parser.Close] / [Close] to
// prevent a goroutine leak.
package clip

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors returned by [Parser.Parse].
var (
	// ErrHelp is returned when the user requests help (--help / -h).
	// The help text has already been written to stdout; callers should
	// exit with status 0.
	ErrHelp = errors.New("help requested")

	// ErrNotRunnable is returned by [Command.Run] when the matched command
	// has no run function registered.
	ErrNotRunnable = errors.New("command not runnable")
)

// errHelpRequest is an internal carrier for the Command context when the user
// passes --help.  It satisfies errors.Is(err, ErrHelp) so callers can use
// errors.Is without caring about the concrete type.
type errHelpRequest struct {
	cmd *Command
	all bool
}

func (e *errHelpRequest) Error() string        { return ErrHelp.Error() }
func (e *errHelpRequest) Is(target error) bool { return target == ErrHelp }

// IOption is the interface a custom option value must satisfy.
// Use [Command.ArgOptionCustom] or [Command.PositionalCustom] to register
// a value that implements it.
type IOption interface {
	String() string
	Parse(s string) error
}

type optSt int

const (
	optStDefault optSt = iota
	optStMustSet
	optStSet
)

// Option describes a single command-line option or positional argument.
// Callers receive a *Option from the registration functions and may chain
// modifier methods ([Option.MustSet], [Option.Hide], etc.) on it.
type Option struct {
	v         IOption
	shortName byte
	longName  string
	argName   string
	desc      string

	hasArg      bool
	incrStep    int
	reverseFlag bool
	hide        bool
	repeatable  bool
	status      optSt
}

// Command represents a (possibly nested) command with its own set of options,
// positionals, and run functions.  The [Parser]'s embedded Command is the
// implicit root; sub-commands are created with [Command.SubCommand].
type Command struct {
	Name, desc string
	longDesc   string
	opts        []*Option
	positionals []*Option
	subcmds     []*Command

	// Arguments holds tokens not consumed as options or positionals —
	// everything after the first unrecognised token when no sub-commands remain,
	// or everything after a bare "--".
	Arguments []string

	run  func(c *Command) error
	init func(c *Command) error
	fini func(c *Command) error

	hide   bool
	parent *Command

	logfilePath  string
	logfileMaxSz int64
	logfile      *os.File
	logger       *log.Logger
	logC         chan string
	logDoneC     chan struct{}
}

// Parser holds all state for one argument-parsing session.  Create one with
// [New]; or use the package-level functions which operate on [DefaultParser].
//
// Parser embeds [Command] so all Command registration methods (FlagOption,
// SubCommand, Positional, …) are directly callable on *Parser.
type Parser struct {
	Command               // root command; all Command methods are promoted
	Args       []string   // populated by Parse; reset on every call
	helpOption Option
	progInfo   string
	logBufSize int
}

// New returns a Parser ready to use, with the default help flag (-h/--help)
// and a log-channel buffer of 64 entries.
func New() *Parser {
	return &Parser{
		helpOption: Option{shortName: 'h', longName: "help", desc: "Help information"},
		logBufSize: 64,
	}
}

// DefaultParser is the Parser that backs the package-level convenience
// functions (Parse, FlagOption, SubCommand, …).  Programs that only ever need
// one parser may use those functions directly instead of calling New().
var DefaultParser = New()

// ProgDescription sets the summary line shown at the top of root-level help.
func (p *Parser) ProgDescription(desc string) { p.progInfo = desc }

// SetHelpOption customises the short/long name of the auto-generated help flag.
// Must be called before Parse.
func (p *Parser) SetHelpOption(shortName byte, longName string) {
	p.helpOption.shortName = shortName
	p.helpOption.longName = longName
}

// SetLogBufSize sets the capacity of the internal log channel.  Must be called
// before Parse.  Returns p so calls can be chained.
func (p *Parser) SetLogBufSize(n int) *Parser {
	p.logBufSize = n
	return p
}

// OpenLogfile configures log output to path with optional size-based rotation.
// maxSize accepts a plain integer (bytes) or a number with suffix k/K, m/M, g/G.
// Pass "" to disable rotation.  Must be called before Parse.
func (p *Parser) OpenLogfile(path, maxSize string) error {
	p.Command.logfilePath = path
	var err error
	p.Command.logfileMaxSz, err = parseSize(maxSize)
	return err
}

// Close shuts down the background logging goroutine and waits for it to drain.
// [Command.Run] calls this automatically.  Call it explicitly when you exit
// before calling Run to prevent a goroutine leak.
func (p *Parser) Close() {
	p.Command.closeLogfile()
}

// Parse processes the argument vector and returns the matched Command.
//
// If args is nil or empty, os.Args is used.  Element [0] is always treated as
// the program name (included in Parser.Args at index 0) and skipped during
// option parsing.  A bare "--" token stops option processing; all subsequent
// tokens are placed in Command.Arguments.
//
// If the user passes --help or -h, Parse prints help to stdout and returns
// (nil, ErrHelp).  Call Close if you do not subsequently call Command.Run.
func (p *Parser) Parse(args []string) (*Command, error) {
	// Start the logging goroutine once per Parse/Close cycle.
	if p.Command.logC == nil {
		p.Command.logC = make(chan string, p.logBufSize)
		p.Command.logDoneC = make(chan struct{})
		go logfunc(&p.Command)
	}

	// Append the help option exactly once even if Parse is called again on the
	// same Parser without an intervening Close/Run.
	hasHelp := false
	for _, o := range p.Command.opts {
		if o == &p.helpOption {
			hasHelp = true
			break
		}
	}
	if !hasHelp && (p.helpOption.shortName != 0 || len(p.helpOption.longName) > 0) {
		p.Command.opts = append(p.Command.opts, &p.helpOption)
	}

	if len(args) == 0 {
		args = os.Args
	}
	// Reset so repeated Parse calls never accumulate stale entries.
	p.Args = nil
	for _, s := range args {
		if len(s) > 0 {
			p.Args = append(p.Args, s)
		}
	}

	cmd, err := parseCommand(&p.Command, p.Args[1:], &p.helpOption)
	if err != nil {
		var hr *errHelpRequest
		if errors.As(err, &hr) {
			p.HelpCommand(hr.cmd, hr.all)
			return nil, ErrHelp
		}
		return nil, err
	}
	return cmd, nil
}

// HelpCommand prints help for c to stdout.  Pass nil to print root-level help.
func (p *Parser) HelpCommand(c *Command, all bool) {
	var lst [][2]string
	if c == nil {
		c = &p.Command
	}
	if c == &p.Command {
		fmt.Printf("%s\n\n", FormatText(p.progInfo, 80, 0, 0))
	} else {
		s := c.longDesc
		if s == "" {
			s = c.desc
		}
		lst = append(lst, [2]string{c.Name, s})
		if prtList(lst, "") > 0 {
			fmt.Println()
		}
		lst = nil
	}
	prtOptions(c.opts, "Options", all, &p.helpOption)
	prtOptions(c.positionals, "Positionals", all, &p.helpOption)
	for _, sc := range c.subcmds {
		if all || !sc.hide {
			lst = append(lst, [2]string{fmt.Sprintf("  %s", sc.Name), sc.desc})
		}
	}
	if prtList(lst, "Sub-Commands") > 0 {
		fmt.Println()
	}
}

// --- Package-level convenience wrappers (all delegate to DefaultParser) ------

func ArgOption(v interface{}, shortName byte, longName, argName, desc string) *Option {
	return DefaultParser.ArgOption(v, shortName, longName, argName, desc)
}
func ArgOptionCustom(v IOption, shortName byte, longName, argName, desc string) *Option {
	return DefaultParser.ArgOptionCustom(v, shortName, longName, argName, desc)
}
func FlagOption(v *bool, shortName byte, longName, desc string) *Option {
	return DefaultParser.FlagOption(v, shortName, longName, desc)
}
func IncrOption(v *int, shortName byte, longName, desc string) *Option {
	return DefaultParser.IncrOption(v, shortName, longName, desc)
}
func Positional(v interface{}, name, desc string) *Option {
	return DefaultParser.Positional(v, name, desc)
}
func SubCommand(name, desc, longDesc string) *Command {
	return DefaultParser.SubCommand(name, desc, longDesc)
}
func SetRuns(run, init, fini func(c *Command) error) *Command {
	return DefaultParser.SetRuns(run, init, fini)
}
func OpenLogfile(path, maxSize string) error  { return DefaultParser.OpenLogfile(path, maxSize) }
func Close()                                   { DefaultParser.Close() }
func Parse(args []string) (*Command, error)   { return DefaultParser.Parse(args) }
func ProgDescription(desc string)              { DefaultParser.ProgDescription(desc) }
func SetHelpOption(shortName byte, longName string) {
	DefaultParser.SetHelpOption(shortName, longName)
}
func HelpCommand(c *Command, all bool) { DefaultParser.HelpCommand(c, all) }

// --- Command registration methods --------------------------------------------

// optConv maps a typed pointer to the corresponding IOption wrapper.
func optConv(v interface{}) IOption {
	switch v := v.(type) {
	case *bool:          return (*clipBool)(v)
	case *int:           return (*clipInt)(v)
	case *int8:          return (*clipInt8)(v)
	case *int16:         return (*clipInt16)(v)
	case *int32:         return (*clipInt32)(v)
	case *int64:         return (*clipInt64)(v)
	case *uint:          return (*clipUint)(v)
	case *uint8:         return (*clipUint8)(v)
	case *uint16:        return (*clipUint16)(v)
	case *uint32:        return (*clipUint32)(v)
	case *uint64:        return (*clipUint64)(v)
	case *float32:       return (*clipFloat32)(v)
	case *float64:       return (*clipFloat64)(v)
	case *string:        return (*clipString)(v)
	case *time.Duration: return (*clipDura)(v)
	case *net.IP:        return (*clipIP)(v)
	default:
		panic(fmt.Sprintf("use _Custom() for Option type %T", v))
	}
}

func (c *Command) Hide() *Command { c.hide = true; return c }

// Positional registers a positional argument on c.  Panics if c already has
// sub-commands.
func (c *Command) Positional(v interface{}, name, desc string) *Option {
	if len(c.subcmds) > 0 {
		panic(fmt.Sprintf("command %s trying to add positional and sub-commands", c.Name))
	}
	o := &Option{v: optConv(v), longName: name, desc: desc}
	c.positionals = append(c.positionals, o)
	return o
}

// PositionalCustom is like [Command.Positional] for values implementing
// [IOption] directly.  Panics if c already has sub-commands.
func (c *Command) PositionalCustom(v IOption, name, desc string) *Option {
	if len(c.subcmds) > 0 {
		panic(fmt.Sprintf("command %s trying to add positional and sub-commands", c.Name))
	}
	o := &Option{v: v, longName: name, desc: desc}
	c.positionals = append(c.positionals, o)
	return o
}

func (c *Command) appendOption(o *Option) *Option {
	c.opts = append(c.opts, o)
	return o
}

func (c *Command) ArgOption(v interface{}, shortName byte, longName, argName, desc string) *Option {
	return c.appendOption(&Option{
		v: optConv(v), shortName: shortName, longName: longName,
		argName: argName, desc: desc, hasArg: true,
	})
}

func (c *Command) ArgOptionCustom(v IOption, shortName byte, longName, argName, desc string) *Option {
	return c.appendOption(&Option{
		v: v, shortName: shortName, longName: longName,
		argName: argName, desc: desc, hasArg: true,
	})
}

func (c *Command) FlagOption(v *bool, shortName byte, longName, desc string) *Option {
	return c.appendOption(&Option{v: (*clipBool)(v), shortName: shortName, longName: longName, desc: desc})
}

func (c *Command) IncrOption(v *int, shortName byte, longName, desc string) *Option {
	return c.appendOption(&Option{
		v: (*clipInt)(v), shortName: shortName, longName: longName,
		desc: desc, incrStep: 1, repeatable: true,
	})
}

// SubCommand creates a child command under c.  Panics if c already has
// positional arguments.
func (c *Command) SubCommand(name, desc, longDesc string) *Command {
	if len(c.positionals) > 0 {
		panic(fmt.Sprintf("command %s trying to add positional and sub-commands", c.Name))
	}
	sc := &Command{Name: name, desc: desc, longDesc: longDesc, parent: c}
	c.subcmds = append(c.subcmds, sc)
	return sc
}

func (c *Command) SetRuns(run, init, fini func(c *Command) error) *Command {
	c.run, c.init, c.fini = run, init, fini
	return c
}

// --- Option modifiers --------------------------------------------------------

func (o *Option) SetIncrStep(step int) *Option {
	if o.incrStep == 0 {
		panic("cannot set increment step on non-increment Option")
	}
	if step == 0 {
		panic("increment step cannot be 0")
	}
	o.incrStep = step
	return o
}

func (o *Option) ReverseFlag() *Option {
	if _, ok := o.v.(*clipBool); !ok {
		panic("ReverseFlag on non-bool Option")
	}
	o.reverseFlag = true
	return o
}

func (o *Option) Hide() *Option       { o.hide = true; return o }
func (o *Option) Repeatable(r bool) *Option { o.repeatable = r; return o }
func (o *Option) MustSet() *Option    { o.status = optStMustSet; return o }

// --- Logging -----------------------------------------------------------------

// closeLogfile signals the logging goroutine to stop and waits for it to exit.
func (c *Command) closeLogfile() {
	if c.logC != nil {
		close(c.logC)
		<-c.logDoneC
		close(c.logDoneC)
		c.logC = nil
		c.logDoneC = nil
	}
}

// logfunc is the single goroutine that owns all file I/O for a Command's log
// channel.  It opens the destination lazily on the first message, writes each
// entry, then rotates the file after writing when the size limit is exceeded.
func logfunc(c *Command) {
	for s := range c.logC {
		// Lazily open the log destination on first message.
		if c.logfile == nil {
			if c.logfilePath != "" {
				var err error
				c.logfile, err = os.OpenFile(c.logfilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					fmt.Printf("warn: failed to open log file '%s'\n", c.logfilePath)
					c.logfile = nil
				}
			} else {
				c.logfile = os.Stdout
			}
		}

		// Create the logger once we have a valid destination.
		if c.logfile != nil && c.logger == nil {
			var prefix string
			if len(c.Name) > 0 {
				prefix = fmt.Sprintf("[%s] ", c.Name)
			}
			c.logger = log.New(c.logfile, prefix, log.LstdFlags)
		}

		// Write before checking rotation so the message that crosses the
		// threshold is never lost.
		if c.logger != nil {
			// Pass s as a plain %s argument to avoid re-interpreting any '%'
			// characters in the already-formatted string.
			c.logger.Printf("%s", s)
		}

		// Rotate after writing; next message will open a fresh file.
		if c.logfile != os.Stdout && c.logfile != nil && c.logfileMaxSz > 0 {
			fi, err := c.logfile.Stat()
			if err != nil || fi.Size() > c.logfileMaxSz {
				c.logfile.Close()
				c.logfile = nil
				c.logger = nil
				os.Rename(c.logfilePath, c.logfilePath+".0")
			}
		}
	}

	if c.logfile != os.Stdout && c.logfile != nil {
		c.logfile.Close()
	}
	c.logDoneC <- struct{}{}
}

// Logf sends a formatted log line to the command's log channel.
// The logger prefix (from c.Name) and timestamp are added automatically.
func (c *Command) Logf(format string, v ...interface{}) {
	if c.logC != nil {
		c.logC <- fmt.Sprintf(format, v...)
	}
}

func (c *Command) ErrLogf(format string, v ...interface{}) {
	c.Logf("Error "+format, v...)
}

// --- Run ---------------------------------------------------------------------

// Run walks the command chain from root down to c calling init functions,
// invokes c.run, then calls fini and closes log channels on the way back.
func (c *Command) Run() error {
	var cmds []*Command
	for pc := c; pc != nil; pc = pc.parent {
		cmds = append(cmds, pc)
	}

	var ch chan string
	var err error
	var i int
	for i = len(cmds) - 1; i >= 0; i-- {
		if cmds[i].logC == nil && ch != nil {
			cmds[i].logC = ch
		}
		if cmds[i].init != nil {
			if err = cmds[i].init(cmds[i]); err != nil {
				cmds[i].ErrLogf("%s", err)
				break
			}
		}
		if cmds[i].logC != nil {
			ch = cmds[i].logC
		}
	}

	if err == nil {
		if c.run != nil {
			if c.logC == nil && ch != nil {
				c.logC = ch
			}
			if err = c.run(c); err != nil && c.logC != nil {
				c.ErrLogf("%s", err)
			}
		} else {
			err = ErrNotRunnable
		}
	}

	if i < 0 {
		i = 0
	}
	for ; i < len(cmds); i++ {
		if cmds[i].fini != nil {
			cmds[i].fini(cmds[i])
		}
		// Only close channels that this command owns (logDoneC non-nil).
		// Sub-commands that borrowed the parent's logC have logDoneC == nil.
		if cmds[i].logC != nil && cmds[i].logDoneC != nil {
			cmds[i].closeLogfile()
		}
	}
	return err
}

// --- Argument parsing internals ----------------------------------------------

func errf(format string, args ...interface{}) error {
	return fmt.Errorf("CommandLine: "+format, args...)
}

func setNoArgOption(o *Option) {
	if o.incrStep != 0 {
		if v_, ok := o.v.(*clipInt); ok {
			*(*int)(v_) += o.incrStep
		} else {
			panic("internal: non-integer Option has non-zero incrStep")
		}
	} else {
		if v_, ok := o.v.(*clipBool); ok {
			*(*bool)(v_) = !o.reverseFlag
		} else {
			panic("internal: non-bool Option has zero incrStep")
		}
	}
}

func parseLongOpt(c *Command, name, str string, helpOpt *Option) (consumed int, er error) {
	kv := strings.Split(name, "=")
	set := false
	for _, o := range c.opts {
		if o == helpOpt {
			continue
		}
		if kv[0] != o.longName {
			continue
		}
		if o.status == optStSet && !o.repeatable {
			er = errf("Option '%s' set more than once", kv[0])
			return
		}
		if o.hasArg {
			if len(kv) == 2 {
				if er = o.v.Parse(kv[1]); er != nil {
					return
				}
				consumed = 1
			} else if len(str) > 0 {
				if er = o.v.Parse(str); er != nil {
					return
				}
				consumed = 2
			} else {
				er = errf("option '%s' needs an argument", kv[0])
				return
			}
		} else {
			if len(kv) > 1 {
				er = errf("option '%s' does not take an argument", kv[0])
				return
			}
			setNoArgOption(o)
			consumed = 1
		}
		set, o.status = true, optStSet
		break
	}

	if !set {
		if kv[0] == helpOpt.longName {
			return 0, &errHelpRequest{cmd: c, all: false}
		}
		if kv[0] == "help-a" {
			return 0, &errHelpRequest{cmd: c, all: true}
		}
		if er == nil {
			er = errf("Option '%s' not recognized", kv[0])
		}
		consumed = 0
	}
	return
}

func parseShortOpt(c *Command, name, str string, helpOpt *Option) (consumed int, er error) {
	for len(name) > 0 {
		var o *Option
		for _, o_ := range c.opts {
			if o_ == helpOpt {
				continue
			}
			if name[0] == o_.shortName {
				o = o_
				break
			}
		}
		if o == nil || o.v == nil {
			if helpOpt.shortName != 0 && name[0] == helpOpt.shortName {
				return 0, &errHelpRequest{cmd: c, all: false}
			}
			er = errf("Option '%s' not recognized", name[:1])
			break
		}
		if o.status == optStSet && !o.repeatable {
			er = errf("option '%s' set more than once", name[:1])
			break
		}
		if o.hasArg {
			if len(name) > 1 {
				if er = o.v.Parse(name[1:]); er != nil {
					return
				}
				consumed = 1
				o.status = optStSet
				break
			} else if len(str) > 0 {
				if er = o.v.Parse(str); er != nil {
					return
				}
				consumed = 2
				o.status = optStSet
				break
			} else {
				er = errf("Option '%s' needs an argument", name[:1])
				break
			}
		} else {
			setNoArgOption(o)
			name = name[1:]
			consumed = 1
			o.status = optStSet
		}
	}
	if er != nil {
		consumed = 0
	}
	return
}

func parsePositional(c *Command, str string) (consumed int, er error) {
	for _, o := range c.positionals {
		if o.status == optStSet {
			continue
		}
		if er = o.v.Parse(str); er != nil {
			return
		}
		o.status = optStSet
		consumed = 1
		break
	}
	return
}

func parseSubCommand(c *Command, str string) (consumed int, sc *Command, er error) {
	if len(c.subcmds) == 0 {
		return 0, nil, nil
	}
	// Exact name match wins over any longer prefix match (e.g. "gcloud"
	// selects gcloud, not gcloud-new).
	for _, s := range c.subcmds {
		if s.Name == str {
			return 1, s, nil
		}
	}
	for _, s := range c.subcmds {
		if len(s.Name) > len(str) && strings.HasPrefix(s.Name, str) {
			if sc != nil {
				return 0, nil, fmt.Errorf("ambiguous command '%s'", str)
			}
			sc = s
		}
	}
	if sc != nil {
		consumed = 1
	} else {
		er = fmt.Errorf("'%s' not recognized", str)
	}
	return
}

func doParse(c *Command, ss []string, helpOpt *Option) (consumed int, sc *Command, er error) {
	arg0 := ss[0]
	var arg1 string
	if len(ss) > 1 {
		arg1 = ss[1]
	}
	if arg0[0] == '-' {
		if len(arg0) == 1 {
			fmt.Println("warning: option '-' ignored")
			consumed = 1
		} else if arg0[1] == '-' {
			if len(arg0) > 2 {
				consumed, er = parseLongOpt(c, arg0[2:], arg1, helpOpt)
			}
			// bare "--" is handled in parseCommand before doParse is called
		} else {
			consumed, er = parseShortOpt(c, arg0[1:], arg1, helpOpt)
		}
	} else {
		if consumed, er = parsePositional(c, arg0); er == nil && consumed == 0 {
			consumed, sc, er = parseSubCommand(c, arg0)
		}
	}
	return
}

func checkMustSetOptions(c *Command) error {
	for c != nil {
		for _, o := range c.opts {
			if o.status == optStMustSet {
				return fmt.Errorf("Option '%s' not given", o.longName)
			}
		}
		for _, o := range c.positionals {
			if o.status == optStMustSet {
				return fmt.Errorf("positional '%s' not given", o.longName)
			}
		}
		c = c.parent
	}
	return nil
}

func parseCommand(c *Command, args []string, helpOpt *Option) (*Command, error) {
	var err error
	for len(args) > 0 {
		// "--" ends option processing; remainder goes verbatim into Arguments.
		if args[0] == "--" {
			c.Arguments = args[1:]
			break
		}

		n := 1
		if len(args) > 1 {
			n = 2
		}
		consumed, sc, er := doParse(c, args[:n], helpOpt)
		if er != nil {
			err = er
			c = nil
			break
		}
		if consumed > 0 {
			args = args[consumed:]
			if sc != nil {
				c = sc
			}
		} else {
			c.Arguments = args
			break
		}
	}
	if err == nil {
		if err = checkMustSetOptions(c); err != nil {
			c = nil
		}
	}
	return c, err
}

// --- Help / formatting -------------------------------------------------------

func FormatText(text string, width, indent, indentFrom uint) string {
	var buf bytes.Buffer
	indstr := "\n"
	if indent > 0 {
		buf.WriteByte('\n')
		for i := 0; i < int(indent); i++ {
			buf.WriteByte(' ')
		}
		indstr = buf.String()
		buf.Reset()
		if indentFrom == 0 {
			buf.Write([]byte(indstr[1:]))
		}
	}
	var w, wlen int
	var word string
	for len(text) > 0 {
		if ix := strings.IndexAny(text, " "); ix >= 0 {
			wlen = ix + 1
			word = text[:wlen]
			text = text[wlen:]
		} else {
			wlen = len(text)
			word = text
			text = ""
		}
		if w+wlen > int(width)+1 {
			buf.WriteString(indstr)
			w = wlen
		} else {
			w += wlen
		}
		buf.WriteString(word)
	}
	return buf.String()
}

func prtList(lst [][2]string, kind string) (n int) {
	var w int
	for _, e := range lst {
		if w < len(e[0]) && len(e[0]) < 32 {
			w = len(e[0])
		}
	}
	if w < 20 {
		w = 20
	}
	if w > 32 {
		w = 32
	}
	w += 2
	for i, o := range lst {
		if i == 0 && kind != "" {
			fmt.Printf("%s:\n\n", kind)
		}
		if len(o[0]) > w-2 {
			fmt.Printf("%s\n", o[0])
			fmt.Printf("%s\n", FormatText(o[1], uint(80-w), uint(w), 0))
		} else {
			fmt.Printf("%-[1]*s", w, o[0])
			fmt.Printf("%s\n", FormatText(o[1], uint(80-w), uint(w), 1))
		}
		n++
	}
	return n
}

func prtOptions(opts []*Option, kind string, all bool, helpOpt *Option) {
	var buf bytes.Buffer
	var lst [][2]string
	var idx int
	for _, o := range opts {
		if !all && o.hide {
			continue
		}
		if o.shortName == helpOpt.shortName && o.longName == helpOpt.longName {
			continue
		}
		buf.Reset()
		buf.WriteString("  ")
		if o.shortName != 0 {
			buf.WriteByte('-')
			buf.WriteByte(o.shortName)
		}
		if len(o.longName) > 0 {
			if o.shortName != 0 {
				buf.WriteByte(',')
			}
			if kind == "Options" {
				fmt.Fprintf(&buf, "--%s", o.longName)
			} else {
				idx++
				fmt.Fprintf(&buf, "%d. %s", idx, o.longName)
			}
		}
		if o.hasArg {
			if o.argName == "" {
				o.argName = "ARG"
			}
			fmt.Fprintf(&buf, " <%s>", o.argName)
		}
		ostr := buf.String()

		buf.Reset()
		buf.WriteString(o.desc)
		if o.v != nil {
			if o.status == optStDefault {
				if dft := o.v.String(); len(dft) > 0 {
					fmt.Fprintf(&buf, " (default: %s)", dft)
				}
			} else if o.status == optStMustSet {
				buf.WriteString(" (must set)")
			}
		}
		lst = append(lst, [2]string{ostr, buf.String()})
	}
	if prtList(lst, kind) > 0 {
		fmt.Println()
	}
}

// parseSize converts a human-readable size string to bytes.
// Recognised suffixes: k/K (×1024), m/M (×1024²), g/G (×1024³).
// A plain integer with no suffix is returned as-is.
// An empty string returns 0 (callers interpret 0 as "no limit").
func parseSize(sz string) (n int64, err error) {
	var factor int64
	if len(sz) > 0 {
		switch sz[len(sz)-1] {
		case 'k', 'K':
			factor = 1024
		case 'm', 'M':
			factor = 1024 * 1024
		case 'g', 'G':
			factor = 1024 * 1024 * 1024
		}
		if factor > 0 {
			sz = sz[:len(sz)-1]
		}
		n, err = strconv.ParseInt(sz, 0, 64)
		if err == nil {
			if factor > 0 {
				n *= factor
			}
		} else {
			err = fmt.Errorf("invalid log size %s", sz)
		}
	}
	return
}
