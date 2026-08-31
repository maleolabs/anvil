package spksshtransport

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"time"
)

// LatencySample is one push observation.
type LatencySample struct {
	ArtifactID string        `json:"artifact_id"`
	Duration   time.Duration `json:"duration"`
	Success    bool          `json:"success"`
	Kind       string        `json:"kind"` // failure classification
	SizeBytes  int64         `json:"size_bytes"`
}

// Histogram collects latency samples and computes percentiles.
type Histogram struct {
	Samples []LatencySample `json:"samples"`
}

// Add records a sample.
func (h *Histogram) Add(s LatencySample) {
	h.Samples = append(h.Samples, s)
}

// SortedDurations returns successful durations sorted ascending.
func (h *Histogram) SortedDurations() []time.Duration {
	var d []time.Duration
	for _, s := range h.Samples {
		if s.Success {
			d = append(d, s.Duration)
		}
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d
}

// Percentile returns p-th percentile (0-100) via nearest-rank.
func (h *Histogram) Percentile(p float64) time.Duration {
	d := h.SortedDurations()
	if len(d) == 0 {
		return 0
	}
	if p <= 0 {
		return d[0]
	}
	if p >= 100 {
		return d[len(d)-1]
	}
	// nearest-rank: ceil(p/100 * N)
	rank := int(math.Ceil(p/100*float64(len(d)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(d) {
		rank = len(d) - 1
	}
	return d[rank]
}

// P50 returns median.
func (h *Histogram) P50() time.Duration { return h.Percentile(50) }

// P95 returns 95th percentile.
func (h *Histogram) P95() time.Duration { return h.Percentile(95) }

// P99 returns 99th.
func (h *Histogram) P99() time.Duration { return h.Percentile(99) }

// SuccessCount returns number of successful pushes.
func (h *Histogram) SuccessCount() int {
	n := 0
	for _, s := range h.Samples {
		if s.Success {
			n++
		}
	}
	return n
}

// FailureCount returns failures.
func (h *Histogram) FailureCount() int { return len(h.Samples) - h.SuccessCount() }

// FailureClassification returns map kind -> count.
func (h *Histogram) FailureClassification() map[string]int {
	m := make(map[string]int)
	for _, s := range h.Samples {
		if !s.Success {
			k := s.Kind
			if k == "" {
				k = "unknown"
			}
			m[k]++
		}
	}
	return m
}

// Buckets returns histogram buckets in ms: 0-100,100-500,500-1000,1000-5000,5000-10000,10000-30000,30000+
func (h *Histogram) Buckets() []Bucket {
	defs := []BucketDef{
		{"0-100ms", 0, 100},
		{"100-500ms", 100, 500},
		{"500ms-1s", 500, 1000},
		{"1s-5s", 1000, 5000},
		{"5s-10s", 5000, 10000},
		{"10s-30s", 10000, 30000},
		{"30s+", 30000, math.MaxInt64},
	}
	buckets := make([]Bucket, len(defs))
	for i, d := range defs {
		buckets[i] = Bucket{Label: d.Label, LowMs: d.LowMs, HighMs: d.HighMs}
	}
	for _, s := range h.Samples {
		if !s.Success {
			continue
		}
		ms := s.Duration.Milliseconds()
		for i, d := range defs {
			if ms >= d.LowMs && ms < d.HighMs {
				buckets[i].Count++
				break
			}
		}
	}
	return buckets
}

// BucketDef defines bucket range.
type BucketDef struct {
	Label  string
	LowMs  int64
	HighMs int64
}

// Bucket is a histogram bucket.
type Bucket struct {
	Label  string `json:"label"`
	LowMs  int64  `json:"low_ms"`
	HighMs int64  `json:"high_ms"`
	Count  int    `json:"count"`
}

// WriteCSV writes histogram.csv with per-sample rows + summary footer.
func (h *Histogram) WriteCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	// header
	if err := cw.Write([]string{"artifact_id", "duration_ms", "success", "kind", "size_bytes"}); err != nil {
		return err
	}
	for _, s := range h.Samples {
		record := []string{
			s.ArtifactID,
			fmt.Sprintf("%d", s.Duration.Milliseconds()),
			fmt.Sprintf("%t", s.Success),
			s.Kind,
			fmt.Sprintf("%d", s.SizeBytes),
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	// summary rows
	_ = cw.Write([]string{})
	_ = cw.Write([]string{"summary", "value"})
	_ = cw.Write([]string{"total", fmt.Sprintf("%d", len(h.Samples))})
	_ = cw.Write([]string{"success", fmt.Sprintf("%d", h.SuccessCount())})
	_ = cw.Write([]string{"failure", fmt.Sprintf("%d", h.FailureCount())})
	_ = cw.Write([]string{"p50_ms", fmt.Sprintf("%d", h.P50().Milliseconds())})
	_ = cw.Write([]string{"p95_ms", fmt.Sprintf("%d", h.P95().Milliseconds())})
	_ = cw.Write([]string{"p99_ms", fmt.Sprintf("%d", h.P99().Milliseconds())})
	_ = cw.Write([]string{"p95_within_30s", fmt.Sprintf("%t", h.P95() < 30*time.Second)})
	for _, b := range h.Buckets() {
		_ = cw.Write([]string{fmt.Sprintf("bucket_%s", b.Label), fmt.Sprintf("%d", b.Count)})
	}
	for k, v := range h.FailureClassification() {
		_ = cw.Write([]string{fmt.Sprintf("fail_%s", k), fmt.Sprintf("%d", v)})
	}
	cw.Flush()
	return cw.Error()
}

// WriteCSVFile writes to path.
func (h *Histogram) WriteCSVFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return h.WriteCSV(f)
}
