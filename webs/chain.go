// Copyright (c) 2016 Alan Kang. All rights reserved.
// Chain of middlewares support for httpmux

package webs

import (
    "net/http"
)

type Chain struct {
    mws     []Handler // slice of middlewares
    h       Handler   // final handler of request
    nextIdx int
}

func NewChain() *Chain {
    return &Chain{}
}

func (c *Chain) Prepend(mws ...Handler) *Chain {
    var m []Handler
    m = append(m, mws...)
    m = append(m, c.mws...)
    c.mws = m
    return c
}

func (c *Chain) PrependFunc(mws ...HandlerFunc) *Chain {
    var m []Handler
    for _, hf := range mws {
        m = append(m, hf)
    }
    return c.Prepend(m...)
}

func (c *Chain) Append(mws ...Handler) *Chain {
    c.mws = append(c.mws, mws...)
    return c
}

func (c *Chain) AppendFunc(mws ...HandlerFunc) *Chain {
    var m []Handler
    for _, hf := range mws {
        m = append(m, hf)
    }
    return c.Append(m...)
}

func (c *Chain) Use(h Handler) *Chain {
    c.h = h
    return c
}

func (c *Chain) UseFunc(hf HandlerFunc) *Chain {
    c.h = hf
    return c
}

func (c *Chain) Next(w http.ResponseWriter, req *http.Request, ctx *Context) {
    var h Handler
    if c.nextIdx >= 0 && c.nextIdx < len(c.mws) {
        h = c.mws[c.nextIdx]
    } else if c.nextIdx == len(c.mws) {
        h = c.h
    } else {
        return
    }

    c.nextIdx++

    if h != nil {
        h.ServeHTTP(w, req, ctx)
        c.Next(w, req, ctx)
    }
}

func (c *Chain) ServeHTTP(w http.ResponseWriter, req *http.Request, ctx *Context) {
    c.nextIdx = 0
    c.Next(w, req, ctx)
}
