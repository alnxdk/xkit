// Copyright (c) 2016 Alan Kang. All rights reserved.
// Context stores URL parameters, params can be accessed by index
// or name if named.
// ctx.Vars can be used to store random data by handlers, won't be used
// by httpmux

package webs

type sectionValue struct {
    s    *section
    vals []string
}

type Context struct {
    paramMap map[string][]string
    Vars     map[string]string

    ServeFileRoot string

    path     []sectionValue
}

func (ctx *Context) setParams() {
    for _, sv := range ctx.path {
        if len(sv.s.sName) > 0 {
            if ctx.paramMap == nil {
                ctx.paramMap = make(map[string][]string)
            }
            ctx.paramMap[sv.s.sName] = sv.vals
        }
    }
}

func (ctx *Context) ParamsByName(name string) []string {
    if p, ok := ctx.paramMap[name]; ok {
        return p
    }
    return nil
}

func (ctx *Context) ParamByName(name string) string {
    vals := ctx.ParamsByName(name)
    if len(vals) > 0 {
        return vals[0]
    }
    return ""
}
