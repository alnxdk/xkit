// Copyright (c) 2016 Alan Kang. All rights reserved.

package webs

import (
    "regexp"
    "strings"
)

type section struct {
    // section name without leading char if not raw type
    sName  string
    sType  sectionType
    regexp *regexp.Regexp
    // only one non-raw sub section is allowed, raw and non-raw sub sections
    // can co-exist, and raw type sub section take higher priority when
    // matching
    hasNonRawSub bool
    subs         map[string]*section
    ts           bool // trailing slash, useful if this is last section
    chain        Chain
}

type sectionType int

const (
    SectionTypeRaw sectionType = iota
    SectionTypeWildCard
    SectionTypeMatch
    SectionTypeRegexp
)

func (s sectionType) String() string {
    var n string
    switch s {
    case SectionTypeRaw:
        n = "SectionTypeRaw"
    case SectionTypeWildCard:
        n = "SectionTypeWildCard"
    case SectionTypeMatch:
        n = "SectionTypeMatch"
    case SectionTypeRegexp:
        n = "SectionTypeRexexp"
    default:
        n = "SectionTypeUnknown"
    }
    return n
}

// echo secion in the URL can be one of four types
//  SectionTypeRaw:
//    normal text which contains no special charactor, and will be treated as is
//  SectionTypeWildCard:
//  SectionTypeMatch:
//  SectionTypeRegexp:

func newSection(m *Mux, sParent *section, name string) (*section, string) {
    s := &section{}
    switch name[0] {
    case ':':
        s.sType = SectionTypeMatch
        s.sName = name[1:]
    case '*':
        s.sType = SectionTypeWildCard
        s.sName = name[1:]
        m.hasWildcard = true
    case '#':
        s.sType = SectionTypeRegexp
        var re string
        if len(name) == 1 {
            return nil, "regexp empty"
        }
        if name[1] == '{' {
            if i := strings.Index(name, "}"); i == -1 {
                return nil, "regexp format error"
            } else {
                s.sName = name[2:i]
                re = name[i+1:]
            }
        } else {
            re = name[1:]
        }
        if len(re) == 0 {
            return nil, "regexp empty"
        }
        var err error
        s.regexp, err = regexp.Compile(re)
        if err != nil {
            return nil, "regexp compile error"
        }
    default:
        s.sType = SectionTypeRaw
        s.sName = name
    }

    if sParent != nil {
        if sParent.sType == SectionTypeWildCard {
            return nil, "wildcard not the last section"
        }
        if s.sType != SectionTypeRaw {
            if sParent.hasNonRawSub {
                return nil, "multiple non raw section"
            }
            s.hasNonRawSub = true
        }
    }

    return s, ""
}

func (rs *section) addRoute(m *Mux, path string, h Handler) {
    // verify arguments not empty
    if hf, ok := h.(HandlerFunc); ok {
        if hf == nil {
            panic("nil handler function")
        }
    } else if c, ok := h.(*Chain); ok {
        if c == nil {
            panic("nil chain handler")
        } else if c.h == nil {
            panic("chain has nil handler")
        }
    } else if h == nil {
        panic("nil handler")
    }
    if len(path) == 0 || path[0] != '/' {
        panic("path must begin with '/'")
    }

    s := rs
    ps := strings.Split(path, "/")
    for _, p := range ps {
        if len(p) == 0 {
            continue
        }
        if s.subs == nil {
            s.subs = make(map[string]*section)
        }

        p = strings.ToLower(p)
        ss, ok := s.subs[p]
        if !ok {
            errmsg := ""
            if ss, errmsg = newSection(m, s, p); errmsg != "" {
                panic("error: addRoute: " + path + " " + errmsg)
            }
            s.subs[p] = ss
        }
        s = ss
    }

    if s.chain.h != nil {
        panic("handler for path " + path + " redefined")
    }

    if c, ok := h.(*Chain); ok {
        s.chain.Append(c.mws...)
        s.chain.h = c.h
    } else {
        s.chain.h = h
    }

    if s.chain.h == nil {
        panic("handler for path " + path + " not defined")
    }

    if s != rs {
        s.ts = strings.HasSuffix(path, "/")
    }
}

func findRoute(m *Mux, ctx *Context, rs *section, path string) bool {
    ctx.path = append(ctx.path, sectionValue{rs, nil})
    ps := strings.Split(path, "/")
    pstrim := ps[:0]
    for _, p := range ps {
        if ptrim := strings.TrimSpace(p); len(ptrim) > 0 {
            pstrim = append(pstrim, ptrim)
        }
    }
    if len(pstrim) == 0 {
        return true
    }

    skipWildcard := true
    for {
        for _, ss := range rs.subs {
            if findRoute_r(ctx, ss, pstrim, skipWildcard) {
                return true
            }
        }
        if skipWildcard && m.hasWildcard {
            skipWildcard = false
        } else {
            break
        }
    }
    ctx.path = ctx.path[:len(ctx.path)-1]
    return false
}

func findRoute_r(ctx *Context, s *section, path []string, skipWildcard bool) bool {
    matched, byWildcard, vals :=  s.match(path, skipWildcard)
    if matched {
        ctx.path = append(ctx.path, sectionValue{s, vals})

        path = path[1:]
        if len(path) == 0 || byWildcard {
            return true
        } else {
            for _, ss := range s.subs {
                if found := findRoute_r(ctx, ss, path, skipWildcard); found {
                    return true
                }
            }
        }

        ctx.path = ctx.path[:len(ctx.path)-1]
    }
    return false
}

func (s *section) match(p []string, skipWildcard bool) (matched, byWildcard bool, vals []string) {
    switch s.sType {
    case SectionTypeRaw:
        if s.sName == p[0] {
            vals = append(vals, p[0])
            return true, false, vals
        }
    case SectionTypeMatch:
        vals = append(vals, p[0])
        return true, false, vals
    case SectionTypeRegexp:
        if vals = s.regexp.FindStringSubmatch(p[0]); vals != nil {
            return true, false, vals
        }
    case SectionTypeWildCard:
        if !skipWildcard {
            vals = append(vals, strings.Join(p, "/"))
            return true, true, vals
        }
    default:
    }
    return false, false, vals
}
