package clip

import (
    "fmt"
    "strconv"
    "time"
    "net"
)

type (
    clipInt    int
    clipInt8   int8
    clipInt16  int16
    clipInt32  int32
    clipInt64  int64
    clipUint   uint
    clipUint8  uint8
    clipUint16 uint16
    clipUint32 uint32
    clipUint64 uint64

    clipBool   bool

    clipFloat32 float32
    clipFloat64 float64

    clipString string

    clipDura time.Duration

    clipIP  net.IP
)

func (i *clipInt) String() string    { return fmt.Sprintf("%d", *i) }
func (i *clipInt8) String() string   { return fmt.Sprintf("%d", *i) }
func (i *clipInt16) String() string  { return fmt.Sprintf("%d", *i) }
func (i *clipInt32) String() string  { return fmt.Sprintf("%d", *i) }
func (i *clipInt64) String() string  { return fmt.Sprintf("%d", *i) }
func (i *clipUint) String() string   { return fmt.Sprintf("%d", *i) }
func (i *clipUint8) String() string  { return fmt.Sprintf("%d", *i) }
func (i *clipUint16) String() string { return fmt.Sprintf("%d", *i) }
func (i *clipUint32) String() string { return fmt.Sprintf("%d", *i) }
func (i *clipUint64) String() string { return fmt.Sprintf("%d", *i) }

func (i *clipInt) Parse(s string) (err error) {
    if s, err := strconv.ParseInt(s, 0, 0); err == nil {
        *i = clipInt(s)
    }
    return
}

func (i *clipInt8) Parse(s string) (err error) {
    v, err := strconv.ParseInt(s, 0, 8)
    if err == nil {
        *i = clipInt8(v)
    }
    return
}

func (i *clipInt16) Parse(s string) (err error) {
    v, err := strconv.ParseInt(s, 0, 16)
    if err == nil {
        *i = clipInt16(v)
    }
    return
}

func (i *clipInt32) Parse(s string) (err error) {
    v, err := strconv.ParseInt(s, 0, 32)
    if err == nil {
        *i = clipInt32(v)
    }
    return
}

func (i *clipInt64) Parse(s string) (err error) {
    v, err := strconv.ParseInt(s, 0, 64)
    if err == nil {
        *i = clipInt64(v)
    }
    return
}

func (i *clipUint) Parse(s string) (err error) {
    v, err := strconv.ParseUint(s, 0, 0)
    if err == nil {
        *i = clipUint(v)
    }
    return
}

func (i *clipUint8) Parse(s string) (err error) {
    v, err := strconv.ParseUint(s, 0, 8)
    if err == nil {
        *i = clipUint8(v)
    }
    return
}

func (i *clipUint16) Parse(s string) (err error) {
    v, err := strconv.ParseUint(s, 0, 16)
    if err == nil {
        *i = clipUint16(v)
    }
    return
}

func (i *clipUint32) Parse(s string) (err error) {
    v, err := strconv.ParseUint(s, 0, 32)
    if err == nil {
        *i = clipUint32(v)
    }
    return
}

func (i *clipUint64) Parse(s string) (err error) {
    v, err := strconv.ParseUint(s, 0, 64)
    if err == nil {
        *i = clipUint64(v)
    }
    return
}

func (b *clipBool) String() string { return fmt.Sprintf("%v", *b) }

func (b *clipBool) Parse(s string) (err error) {
    v, err := strconv.ParseBool(s)
    if err == nil {
        *b = clipBool(v)
    }
    return
}

func (f *clipFloat32) String() string { return fmt.Sprintf("%g", *f) }

func (f *clipFloat32) Parse(s string) (err error) {
    v, err := strconv.ParseFloat(s, 32)
    if err == nil {
        *f = clipFloat32(v)
    }
    return
}

func (f *clipFloat64) String() string { return fmt.Sprintf("%g", *f) }

func (f *clipFloat64) Parse(s string) (err error) {
    v, err := strconv.ParseFloat(s, 64)
    if err == nil {
        *f = clipFloat64(v)
    }
    return
}

func (s *clipString) String() string { return string(*s) }

func (s *clipString) Parse(ss string) (err error) {
    *s = clipString(ss)
    return
}

func (d *clipDura) String() string { return fmt.Sprintf("%s", time.Duration(*d)) }

func (d *clipDura) Parse(s string) (err error) {
    v, err := time.ParseDuration(s)
    if (err == nil) {
        *d = clipDura(v)
    }
    return
}

func (i *clipIP) String() string { return fmt.Sprintf("%s", net.IP(*i)) }

func (i *clipIP) Parse(s string) (err error) {
    v := net.ParseIP(s)
    if v != nil {
        *i = clipIP(v)
    } else {
        err = fmt.Errorf("'%s' is not valid IP address", s)
    }
    return
}
