// Copyright (c) 2016 Alan Kang. All rights reserved.
package webs

import (
    "net/http"
)

type Mux struct {
    roots map[string]*section
    hasWildcard bool

    NotFound http.Handler

    PanicHandler func(http.ResponseWriter, *http.Request, interface{})
}

type Handler interface {
    ServeHTTP(http.ResponseWriter, *http.Request, *Context)
}

type HandlerFunc func(w http.ResponseWriter, req *http.Request, ctx *Context)

func (hf HandlerFunc) ServeHTTP(w http.ResponseWriter, req *http.Request, ctx *Context) {
    hf(w, req, ctx)
}

func New() *Mux {
    return &Mux{}
}

func (m *Mux) Get(path string, h Handler) {
    m.Handle("GET", path, h)
}

func (m *Mux) Post(path string, h Handler) {
    m.Handle("POST", path, h)
}

func (m *Mux) Head(path string, h Handler) {
    m.Handle("HEAD", path, h)
}

func (m *Mux) Options(path string, h Handler) {
    m.Handle("OPTIONS", path, h)
}

func (m *Mux) Put(path string, h Handler) {
    m.Handle("PUT", path, h)
}

func (m *Mux) Patch(path string, h Handler) {
    m.Handle("PATCH", path, h)
}

func (m *Mux) Delete(path string, h Handler) {
    m.Handle("DELETE", path, h)
}

func (m *Mux) GetFunc(path string, hf HandlerFunc) {
    m.Handle("GET", path, hf)
}

func (m *Mux) PostFunc(path string, hf HandlerFunc) {
    m.Handle("POST", path, hf)
}

func (m *Mux) HeadFunc(path string, hf HandlerFunc) {
    m.Handle("HEAD", path, hf)
}

func (m *Mux) OptionsFunc(path string, hf HandlerFunc) {
    m.Handle("OPTIONS", path, hf)
}

func (m *Mux) PutFunc(path string, hf HandlerFunc) {
    m.Handle("PUT", path, hf)
}

func (m *Mux) PatchFunc(path string, hf HandlerFunc) {
    m.Handle("PATCH", path, hf)
}

func (m *Mux) DeleteFunc(path string, hf HandlerFunc) {
    m.Handle("DELETE", path, hf)
}

func (m *Mux) Handle(method, path string, h Handler) {
    if m.roots == nil {
        m.roots = make(map[string]*section)
    }
    rs, _ := m.roots[method]
    if rs == nil {
        errmsg := ""
        rs, errmsg = newSection(m, nil, "/")
        if errmsg != "" {
            panic(errmsg)
        }
        m.roots[method] = rs
    }
    rs.addRoute(m, path, h)
}

func (m *Mux) ServeFiles(path string, root string, mh Handler) {
    if len(path) < 10 || path[len(path)-10:] != "/*filepath" {
        panic("path must end with /*filepath in path " + path)
    }
    fileServer := http.FileServer(http.Dir(root))
    h := func(w http.ResponseWriter, req *http.Request, ctx *Context) {
        ctx.ServeFileRoot = root
        req.URL.Path = ctx.ParamByName("filepath")
        if mh != nil {
            mh.ServeHTTP(w, req, ctx)
        }
        fileServer.ServeHTTP(w, req)
    }
    m.GetFunc(path, h)
}

func (m *Mux) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    if m.PanicHandler != nil {
        defer m.reCover(w, req)
    }

    path := req.URL.Path

    if rs := m.roots[req.Method]; rs != nil {
        ctx := Context{}
        var c *Chain = nil
        if findRoute(m, &ctx, rs, path) {
            sv := &(ctx.path[len(ctx.path)-1])
            c = &(sv.s.chain)
        }
        if c != nil {
            ctx.setParams()
            c.ServeHTTP(w, req, &ctx)
            return
        }
    }

    if m.NotFound != nil {
        m.NotFound.ServeHTTP(w, req)
    } else {
        http.NotFound(w, req)
    }
}

func (m *Mux) reCover(w http.ResponseWriter, req *http.Request) {
    if rc := recover(); rc != nil {
        m.PanicHandler(w, req, rc)
    }
}
