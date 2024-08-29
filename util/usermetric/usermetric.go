// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package usermetric provides a container and handler
// for user-facing metrics.
package usermetric

import (
	"expvar"
	"fmt"
	"io"
	"net/http"
	"strings"

	"tailscale.com/metrics"
	"tailscale.com/tsweb/varz"
)

// Registry tracks user-facing metrics of various Tailscale subsystems.
type Registry struct {
	vars expvar.Map
}

// NewMultiLabelMap creates and register a new
// MultiLabelMap[T] variable with the given name and returns it.
// The variable is registered with the userfacing metrics package.
//
// Note that usermetric are not protected against duplicate
// metrics name. It is the caller's responsibility to ensure that
// the name is unique.
func NewMultiLabelMap[T comparable](m *Registry, name string, promType, helpText string) *metrics.MultiLabelMap[T] {
	if m == nil {
		return nil
	}
	ml := &metrics.MultiLabelMap[T]{
		Type: promType,
		Help: helpText,
	}
	var zero T
	_ = metrics.LabelString(zero) // panic early if T is invalid
	m.vars.Set(name, ml)
	return ml
}

// Gauge is a gauge metric with no labels.
type Gauge struct {
	m    *expvar.Float
	help string
}

// NewGauge creates and register a new gauge metric with the given name and help text.
func (m *Registry) NewGauge(name, help string) *Gauge {
	if m == nil {
		return nil
	}
	g := &Gauge{&expvar.Float{}, help}
	m.vars.Set(name, g)
	return g
}

// Set sets the gauge to the given value.
func (g *Gauge) Set(v float64) {
	if g == nil {
		return
	}
	g.m.Set(v)
}

// String returns the string of the underlying expvar.Float.
// This satisfies the expvar.Var interface.
func (g *Gauge) String() string {
	if g == nil {
		return ""
	}
	return g.m.String()
}

// WritePrometheus writes the gauge metric in Prometheus format to the given writer.
// This satisfies the varz.PrometheusWriter interface.
func (g *Gauge) WritePrometheus(w io.Writer, name string) {
	io.WriteString(w, "# TYPE ")
	io.WriteString(w, name)
	io.WriteString(w, " gauge\n")
	if g.help != "" {
		io.WriteString(w, "# HELP ")
		io.WriteString(w, name)
		io.WriteString(w, " ")
		io.WriteString(w, g.help)
		io.WriteString(w, "\n")
	}

	io.WriteString(w, name)
	fmt.Fprintf(w, " %v\n", g.m.Value())
}

// Handler returns a varz.Handler that serves the userfacing expvar contained
// in this package.
func (m *Registry) Handler(w http.ResponseWriter, r *http.Request) {
	varz.ExpvarDoHandler(m.vars.Do)(w, r)
}

func (m *Registry) String() string {
	var sb strings.Builder
	m.vars.Do(func(kv expvar.KeyValue) {
		fmt.Fprintf(&sb, "%s: %v\n", kv.Key, kv.Value)
	})

	return sb.String()
}
