// Package metrics renders document-service counters in Prometheus text
// format through ardahttp.HandlerChain's extra renderers. Labels never carry
// document content.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"
	"time"
)

var buckets = []float64{1, 2, 5, 10, 15, 20, 25, 30}

type reqKey struct{ docType, outcome, location string }
type flagKey struct{ field, flag string }

type histogram struct {
	counts []uint64
	sum    float64
	count  uint64
}

type Metrics struct {
	mu       sync.Mutex
	requests map[reqKey]uint64
	flags    map[flagKey]uint64
	hist     map[string]*histogram
}

func New() *Metrics {
	return &Metrics{requests: map[reqKey]uint64{}, flags: map[flagKey]uint64{}, hist: map[string]*histogram{}}
}

func (m *Metrics) ObserveRequest(docType, outcome, location string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[reqKey{docType, outcome, location}]++
	h := m.hist[docType]
	if h == nil {
		h = &histogram{counts: make([]uint64, len(buckets))}
		m.hist[docType] = h
	}
	s := d.Seconds()
	for i, le := range buckets {
		if s <= le {
			h.counts[i]++
		}
	}
	h.sum += s
	h.count++
}

func (m *Metrics) ObserveFlag(field, flag string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flags[flagKey{field, flag}]++
}

func (m *Metrics) Render(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fmt.Fprintln(w, "# HELP document_extract_requests_total Document extraction requests by outcome.")
	fmt.Fprintln(w, "# TYPE document_extract_requests_total counter")
	reqs := make([]reqKey, 0, len(m.requests))
	for k := range m.requests {
		reqs = append(reqs, k)
	}
	sort.Slice(reqs, func(i, j int) bool {
		return fmt.Sprint(reqs[i]) < fmt.Sprint(reqs[j])
	})
	for _, k := range reqs {
		fmt.Fprintf(w, "document_extract_requests_total{doc_type=%q,outcome=%q,model_location=%q} %d\n",
			k.docType, k.outcome, k.location, m.requests[k])
	}

	fmt.Fprintln(w, "# HELP document_extract_duration_seconds Document extraction latency.")
	fmt.Fprintln(w, "# TYPE document_extract_duration_seconds histogram")
	types := make([]string, 0, len(m.hist))
	for k := range m.hist {
		types = append(types, k)
	}
	sort.Strings(types)
	for _, dt := range types {
		h := m.hist[dt]
		for i, le := range buckets {
			fmt.Fprintf(w, "document_extract_duration_seconds_bucket{doc_type=%q,le=%q} %d\n",
				dt, strconv.FormatFloat(le, 'f', -1, 64), h.counts[i])
		}
		fmt.Fprintf(w, "document_extract_duration_seconds_bucket{doc_type=%q,le=\"+Inf\"} %d\n", dt, h.count)
		fmt.Fprintf(w, "document_extract_duration_seconds_sum{doc_type=%q} %g\n", dt, h.sum)
		fmt.Fprintf(w, "document_extract_duration_seconds_count{doc_type=%q} %d\n", dt, h.count)
	}

	fmt.Fprintln(w, "# HELP document_field_flags_total Validation flags raised per field.")
	fmt.Fprintln(w, "# TYPE document_field_flags_total counter")
	fl := make([]flagKey, 0, len(m.flags))
	for k := range m.flags {
		fl = append(fl, k)
	}
	sort.Slice(fl, func(i, j int) bool { return fl[i].field+fl[i].flag < fl[j].field+fl[j].flag })
	for _, k := range fl {
		fmt.Fprintf(w, "document_field_flags_total{field=%q,flag=%q} %d\n", k.field, k.flag, m.flags[k])
	}
}
