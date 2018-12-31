package clip

import (
    "fmt"
    "os"
    "time"
    "strings"
    "net"
    "bytes"
    "log"
    "errors"
    "strconv"
)

var ErrNotRunnable = errors.New("command not runnable")

// need pointer receiver
type IOption interface {
    String() string
    Parse(s string) error
}

type optSt int
const (
    optStDefault optSt = iota
    optStMustSet
    optStSet
);

type Option struct {
    v           IOption
    shortName   byte
    longName    string
    desc        string

    hasArg      bool
    incrStep    int

    reverseFlag bool
    hide        bool
    repeatable  bool

    status      optSt
}

type Command struct {
    name, desc  string
    opts        []*Option
    positionals []*Option
    subcmds     []*Command

    Arguments   []string

    run         func(c *Command) error

    hide        bool
    parent     *Command

    logfilePath string
    logfileMaxSz int64
    logfile     *os.File
    logger      *log.Logger
    logC        chan string
    logDoneC    chan struct{}
}

var helpOption = Option{ shortName: 'h', longName: "help",
                    desc: "Command usage" }

var RootCmd Command
var progInfo string

func ArgOption(v interface{}, shortName byte, longName, desc string) *Option {
    return RootCmd.ArgOption(v, shortName, longName, desc)
}

func ArgOptionCustom(v IOption, shortName byte, longName, desc string) *Option {
    return RootCmd.ArgOptionCustom(v, shortName, longName, desc)
}

func FlagOption(v *bool, shortName byte, longName, desc string) *Option {
    return RootCmd.FlagOption(v, shortName, longName, desc)
}

func IncrOption(v *int, shortName byte, longName, desc string) *Option {
    return RootCmd.IncrOption(v, shortName, longName, desc)
}

func Positional(v interface{}, name, desc string) *Option {
    return RootCmd.Positional(v, name, desc)
}

func SubCommand(name, desc string, run func(c *Command) error) *Command {
    return RootCmd.SubCommand(name, desc, run)
}

func SetRun(run func(c *Command) error) {
    RootCmd.run = run
}

func SetLogfile(path string, maxSize string) error {
    return RootCmd.SetLogfile(path, maxSize)
}

func optConv(v interface{}) IOption {
    var ov IOption
    switch v := v.(type) {
    case *bool:   ov = (*clipBool)(v)
    case *int:    ov = (*clipInt)(v)
    case *int8:   ov = (*clipInt8)(v)
    case *int16:  ov = (*clipInt16)(v)
    case *int32:  ov = (*clipInt32)(v)
    case *int64:  ov = (*clipInt64)(v)
    case *uint:   ov = (*clipUint)(v)
    case *uint8:  ov = (*clipUint8)(v)
    case *uint16: ov = (*clipUint16)(v)
    case *uint32: ov = (*clipUint32)(v)
    case *float32:ov = (*clipFloat32)(v)
    case *float64:ov = (*clipFloat64)(v)
    case *string: ov = (*clipString)(v)
    case *time.Duration: ov = (*clipDura)(v)
    case *net.IP: ov = (*clipIP)(v)
    default: panic(fmt.Sprintf("use _Custom() for Option type %T", v))
    }
    return ov
}

func (c *Command) Hide() *Command {
    c.hide = true
    return c
}

func (c *Command) Positional(v interface{}, name, desc string) *Option {
    o := &Option{v: optConv(v), longName: name, desc: desc}
    c.positionals = append(c.positionals, o)
    return o
}

func (c *Command) PositionalCustom(v IOption, name, desc string) *Option {
    o := &Option{v: v, longName: name, desc: desc}
    c.positionals = append(c.positionals, o)
    return o
}

func (c *Command) appendOption(o *Option) *Option {
    if o.shortName == helpOption.shortName {
        helpOption.shortName = 0
    }
    if o.longName == helpOption.longName {
        helpOption.longName = ""
    }
    c.opts = append(c.opts, o)
    return o
}

func (c *Command) ArgOption(v interface{}, shortName byte, longName, desc string) *Option {
    o := &Option{v: optConv(v), shortName: shortName, longName: longName,
                 desc: desc, hasArg: true}
    return c.appendOption(o)
}

func (c *Command) ArgOptionCustom(v IOption, shortName byte, longName, desc string) *Option {
    o := &Option{v: v, shortName: shortName, desc: desc, hasArg: true}
    return c.appendOption(o)
}

func (c *Command) FlagOption(v *bool, shortName byte, longName, desc string) *Option {
    o := &Option{v: (*clipBool)(v), shortName: shortName, longName: longName, desc: desc}
    return c.appendOption(o)
}

func (c *Command) IncrOption(v *int, shortName byte, longName, desc string) *Option {
    o := &Option{v: (*clipInt)(v), shortName: shortName, longName: longName,
        desc: desc, incrStep: 1, repeatable: true}
    return c.appendOption(o)
}

func SetHelpOption(shortName byte, longName string) {
    helpOption.shortName = shortName
    helpOption.longName = longName
}

func (c *Command) SubCommand(name, desc string, run func(c *Command) error) *Command {
    sc := &Command{name: name, desc: desc, parent: c, run: run}
    c.subcmds = append(c.subcmds, sc)
    return sc
}

func (c *Command) SetLogfile(path string, maxSize string) (err error) {
    c.logfilePath = path
    c.logfileMaxSz, err = parseSize(maxSize)
    return
}

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
        default:
        }
        if factor > 0 {
            sz = sz[:len(sz)-1]
        }
        n, err = strconv.ParseInt(sz, 0, 64)
        if err == nil {
            n *= factor
        } else {
            err = fmt.Errorf("invalid log size %s", sz)
        }
    }
    return
}

func (c *Command) Run() error {
    if c.run == nil {
        return ErrNotRunnable
    }
    var err error
    if len(c.logfilePath) > 0 {
        c.logC = make(chan string, 5)
        c.logDoneC = make(chan struct{})
        go logfunc(c)
    }
    err = c.run(c)
    if c.logC != nil {
        if err != nil {
            c.logC <- fmt.Sprintf("%s", err)
        }
        close(c.logC)
        <-c.logDoneC
        close(c.logDoneC)
    }

    return err
}

func logfunc(c *Command) {
    for s := range c.logC {
        if c.logfile == nil {
            var err error
            c.logfile, err = os.OpenFile(c.logfilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
            if err == nil {
                var prefix string
                if len(c.name) > 0 {
                    prefix = fmt.Sprintf("[%s] ", c.name)
                }
                c.logger = log.New(c.logfile, prefix, log.LstdFlags)
            } else {
                fmt.Printf("warn: failed to open log file '%s'", c.logfilePath)
                c.logger = nil
            }
        }

        if c.logfile != nil && c.logfileMaxSz > 0 {
            fi, err := c.logfile.Stat()
            if err != nil || fi.Size() > c.logfileMaxSz {
                c.logfile.Close()
                c.logfile = nil
                os.Rename(c.logfilePath, c.logfilePath + ".0")
            }
        }

        if c.logger != nil {
            c.logger.Printf(s)
        }
    }

    if c.logfile != nil {
        c.logfile.Close()
    }
    c.logDoneC <- struct{}{}
}

func (c *Command) Logf(format string, v ...interface{}) {
    if c.logC != nil {
        c.logC <- fmt.Sprintf(format, v...)
    }
}

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

func (o *Option) Hide() *Option {
    o.hide = true
    return o
}

func (o *Option) Repeatable(r bool) *Option {
    o.repeatable = r
    return o
}

func (o *Option) MustSet() *Option {
    o.status = optStMustSet
    return o
}

func errf(format string, args ...interface{}) error {
    return fmt.Errorf(fmt.Sprintf("CommandLine: %s", format), args...)
}

/*
func prtf(format string, args ...interface{}) {
    fmt.Printf(fmt.Sprintf("CommandLine: %s", format), args...)
}
*/

func setNoArgOption(o *Option) {
    if o.incrStep != 0 {
        if v_, ok := o.v.(*clipInt); ok {
            v := (*int)(v_);
            *v += o.incrStep
        } else {
            panic("internal: none integer Option has non-zero incrStep")
        }
    } else {
        if v_, ok := o.v.(*clipBool); ok {
            v := (*bool)(v_)
            *v = !o.reverseFlag
        } else {
            panic("internal: none boolean Option has zero incrStep")
        }
    }
}

func parseLongOpt(c *Command, name string, str string) (consumed int, er error) {
    kv := strings.Split(name, "=")
    set := false
    for _, o := range c.opts {
        if o == &helpOption {
            continue
        }
        if kv[0] == o.longName {
            if o.status == optStSet && !o.repeatable {
                er = errf("Option '%s' set more than once", kv[0])
                return
            }
            if o.hasArg {
                if len(kv) == 2 {
                    if er = o.v.Parse(kv[1]); er != nil { return }
                    //prtf("Set long Option %s=%s\n", kv[0], kv[1])
                    consumed = 1
                    set, o.status = true, optStSet
                } else if len(str) > 0 {
                    if er = o.v.Parse(str); er != nil { return }
                    //prtf("Set long Option %s=%s\n", kv[0], str)
                    consumed = 2
                    set, o.status = true, optStSet
                } else {
                    er = errf("optino '%s' need an argument", kv[0])
                    return
                }
            } else {
                if len(kv) > 1 {
                    er = errf("optino '%s' does not take argument", kv[0])
                    return
                }
                setNoArgOption(o)
                //prtf("Set long Option %s\n", kv[0])
                consumed = 1
                set, o.status = true, optStSet
            }
        }
        if (set) {
            break
        }
    }

    if !set {
        if kv[0] == helpOption.longName {
            HelpCommand(c, false)
            os.Exit(0)
        } else if kv[0] == "help-a" {
            HelpCommand(c, true)
            os.Exit(0)
        }
        consumed = 0
        if er == nil {
            er = errf("Option '%s' not recognized", kv[0])
        }
    }
    return
}

func parseShortOpt(c *Command, name string, str string) (consumed int, er error) {
    for len(name) > 0 {
        var o *Option
        for _, o_ := range c.opts {
            if o_ == &helpOption {
                continue
            }
            if name[0] == o_.shortName {
                o = o_
                break
            }
        }
        if o == nil || o.v == nil {
            if name[0] == helpOption.shortName {
                HelpCommand(c, false)
                os.Exit(0)
            }
            er = errf("Option '%s' not recognized", name[:1])
            break
        }
        if o.status == optStSet && !o.repeatable {
            er = errf("optino '%s' set more than once", name[:1])
            break
        }

        if o.hasArg {
            if len(name) > 1 {
                if er = o.v.Parse(name[1:]); er != nil { return }
                //prtf("Set short Option %s=%s\n", name[:1], name[1:])
                consumed = 1
                o.status = optStSet
                break
            } else if len(str) > 0 {
                if er = o.v.Parse(str); er != nil { return }
                //prtf("Set short Option %s=%s\n", name[:1], str)
                consumed = 2
                o.status =  optStSet
                break
            } else {
                er = errf("Option '%s' need an argument", name[:1])
                break
            }
        } else {
            setNoArgOption(o)
            //prtf("Set short Option %s\n", name[:1])
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
        if er = o.v.Parse(str); er != nil { return }
        //prtf("Set positianl '%s' to '%s'\n", o.longName, str)
        o.status = optStSet
        consumed = 1
        break
    }
    return
}

func parseSubCommand(c *Command, str string) (consumed int, sc *Command, er error) {
    var scTmp *Command
    for _, s := range c.subcmds {
        scTmp = nil
        if len(s.name) == len(str) {
            if s.name == str {
                scTmp = s
            }
        } else if len(s.name) > len(str) {
            if strings.HasPrefix(s.name, str) {
                scTmp = s
            }
        }
        if scTmp != nil {
            if sc != nil {
                er = fmt.Errorf("ambiguous command '%s'", str)
                sc = nil
                break
            } else {
                sc = scTmp
            }
        }
    }
    if sc != nil {
        consumed = 1
    }
    return
}

func doParse(c *Command, ss []string) (consumed int, sc *Command, er error) {
    arg0 := ss[0]
    var arg1 string
    if len(ss) > 1 {
        arg1 = ss[1]
    }

    if arg0[0] == '-' {
        if len(arg0) == 1 {
            fmt.Println("warning: Option '-' ignored")
            consumed = 1
        } else if arg0[1] == '-' {
            if len(arg0) > 2 {
                consumed, er = parseLongOpt(c, arg0[2:], arg1)
            }
        } else {
            consumed, er = parseShortOpt(c, arg0[1:], arg1)
        }
    } else {
        if consumed, er = parsePositional(c, arg0); er == nil {
            if consumed == 0 {
                consumed, sc, er = parseSubCommand(c, arg0)
            }
        }
    }
    return
}

func checkMustSetOptions(c *Command) error {
    for c != nil {
        for _, o := range c.opts {
            if o.status == optStMustSet {
                return fmt.Errorf("Option '%s' not given", o.longName) //fixme
            }
        }
        for _, o := range c.positionals {
            if o.status == optStMustSet {
                return fmt.Errorf("positional Option '%s' not given", o.longName)
            }
        }
        c = c.parent
    }
    return nil
}

func parseCommand(c *Command, args []string) (*Command, error) {
    var err error
    for len(args) > 0 {
        n := 1
        if len(args) > 1 {
            n = 2
        }
        consumed, sc, er := doParse(c, args[:n])
        if er != nil {
            err = er
            c = nil
            break
        }

        if consumed > 0 {
            args = args[consumed:]
            if sc != nil {
                if len(c.logfilePath) > 0 && len(sc.logfilePath) == 0 {
                    sc.logfilePath = c.logfilePath
                }
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

func Parse() (*Command, error) {
    if helpOption.shortName != 0 || len(helpOption.longName) > 0 {
        RootCmd.opts = append(RootCmd.opts, &helpOption)
    }
    return parseCommand(&RootCmd, os.Args[1:])
}

func formatText(text string, width, indent, indentFrom uint) string {
    var buf bytes.Buffer
    indstr := "\n"

    if indent > 0 {
        buf.WriteByte('\n')
        for i:=0; i<int(indent); i++ {
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
            word = text
            text = ""
        }

        if w + wlen > int(width) + 1 {
            buf.Write([]byte(indstr))
            w = wlen
        } else {
            w += wlen
        }
        buf.Write([]byte(word))
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

    if w < 20 { w = 20 }
    if w > 32 { w = 32 }
    w += 2
    for i, o := range lst {
        if i == 0 {
            fmt.Printf("%s:\n\n", kind)
        }
        if len(o[0]) > w-2 {
            fmt.Printf("%s\n", o[0])
            fmt.Printf("%s\n", formatText(o[1], uint(80-w), uint(w), 0))
        } else {
            fmt.Printf("%-[1]*s", w, o[0])
            fmt.Printf("%s\n", formatText(o[1], uint(80-w), uint(w), 1))
        }
        n++
    }
    return n
}

func prtOptions(os []*Option, kind string, all bool) {
    var buf bytes.Buffer
    var lst [][2]string

    for _, o := range os {
        if !all && o.hide {
            continue
        }
        buf.Reset()
        buf.Write([]byte("  "))
        if o.shortName != 0 {
            buf.WriteByte('-')
            buf.WriteByte(o.shortName)
            buf.WriteByte(',')
        }
        if len(o.longName) > 0 {
            buf.Write([]byte(fmt.Sprintf("--%s", o.longName)))
        }
        ostr := buf.String()

        buf.Reset()
        buf.Write([]byte(o.desc))
        if o.v != nil {
            if o.status == optStDefault {
                dft := o.v.String()
                if len(dft) > 0 {
                    buf.Write([]byte(fmt.Sprintf(" (default: %s)", dft)))
                }
            } else if o.status == optStMustSet {
                buf.Write([]byte(" (must set)"))
            }
        }

        desc := buf.String()
        lst = append(lst, [2]string{ostr, desc})
    }

    if prtList(lst, kind) > 0 {
        fmt.Println()
    }
}

func HelpCommand(c *Command, all bool) {
    if (c == &RootCmd) {
        fmt.Printf("%s\n\n", formatText(progInfo, 80, 0, 0))
    } else {
        fmt.Printf("Command: %s\n", c.name)
    }
    prtOptions(c.opts, "Options", all)
    prtOptions(c.positionals, "Positionals", all)

    var lst [][2]string
    for _, sc := range c.subcmds {
        if all || !sc.hide {
            lst = append(lst, [2]string{fmt.Sprintf("  %s", sc.name), sc.desc})
        }
    }
    if prtList(lst, "Sub-Commands") > 0 {
        fmt.Println()
    }
}

func ProgDescription(desc string) {
    progInfo = desc
}
