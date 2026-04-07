// Package metrics 提供零依赖的 Agent 指标收集层，支持 Prometheus 文本格式导出。
package metrics

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Counter 是一个单调递增的计数器。
type Counter interface {
	Inc()
	Add(n float64)
	Value() float64
}

// Histogram 是一个值分布直方图。
type Histogram interface {
	Observe(v float64)
	Snapshot() HistogramSnapshot
}

// HistogramSnapshot 是直方图在某一时刻的快照。
type HistogramSnapshot struct {
	Count  uint64
	Sum    float64
	Buckets []BucketCount // 按上界从小到大排列
}

// BucketCount 记录上界及落入该桶的累计数量。
type BucketCount struct {
	UpperBound float64
	Count      uint64
}

// MetricFamily 是单个指标的元信息与采样值。
type MetricFamily struct {
	Name    string
	Help    string
	Type    string // "counter" | "histogram"
	Samples []Sample
}

// Sample 是一个带标签集合的指标采样值。
type Sample struct {
	Labels map[string]string
	Value  float64
}

// Collector 是指标收集器接口。
type Collector interface {
	Counter(name string) Counter
	Histogram(name string, buckets ...float64) Histogram
	Snapshot() []MetricFamily
	// ExportPromText 导出 Prometheus 文本格式。
	ExportPromText() string
}

// ---- 内存实现 -----------------------------------------------------------

// MemoryCollector 是基于原子操作的内存指标收集器。
type MemoryCollector struct {
	mu         sync.RWMutex
	counters   map[string]*atomicCounter
	histograms map[string]*atomicHistogram
	order      []string // 按注册顺序保存名称
}

// NewMemoryCollector 创建一个新的内存指标收集器。
func NewMemoryCollector() *MemoryCollector {
	return &MemoryCollector{
		counters:   make(map[string]*atomicCounter),
		histograms: make(map[string]*atomicHistogram),
	}
}

func (c *MemoryCollector) Counter(name string) Counter {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cnt, ok := c.counters[name]; ok {
		return cnt
	}
	cnt := &atomicCounter{}
	c.counters[name] = cnt
	c.order = append(c.order, "c:"+name)
	return cnt
}

// DefaultBuckets 是默认直方图分桶（秒）。
var DefaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

func (c *MemoryCollector) Histogram(name string, buckets ...float64) Histogram {
	c.mu.Lock()
	defer c.mu.Unlock()
	if h, ok := c.histograms[name]; ok {
		return h
	}
	if len(buckets) == 0 {
		buckets = DefaultBuckets
	}
	h := newAtomicHistogram(buckets)
	c.histograms[name] = h
	c.order = append(c.order, "h:"+name)
	return h
}

func (c *MemoryCollector) Snapshot() []MetricFamily {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var families []MetricFamily
	for _, key := range c.order {
		kind, name := key[0:1], key[2:]
		switch kind {
		case "c":
			if cnt, ok := c.counters[name]; ok {
				families = append(families, MetricFamily{
					Name: name,
					Type: "counter",
					Samples: []Sample{{Value: cnt.Value()}},
				})
			}
		case "h":
			if h, ok := c.histograms[name]; ok {
				snap := h.Snapshot()
				fam := MetricFamily{Name: name, Type: "histogram"}
				for _, b := range snap.Buckets {
					fam.Samples = append(fam.Samples, Sample{
						Labels: map[string]string{"le": formatFloat(b.UpperBound)},
						Value:  float64(b.Count),
					})
				}
				fam.Samples = append(fam.Samples, Sample{
					Labels: map[string]string{"le": "+Inf"},
					Value:  float64(snap.Count),
				})
				fam.Samples = append(fam.Samples, Sample{
					Labels: map[string]string{"__sum__": ""},
					Value:  snap.Sum,
				})
				fam.Samples = append(fam.Samples, Sample{
					Labels: map[string]string{"__count__": ""},
					Value:  float64(snap.Count),
				})
				families = append(families, fam)
			}
		}
	}
	return families
}

func (c *MemoryCollector) ExportPromText() string {
	var sb strings.Builder
	for _, fam := range c.Snapshot() {
		sb.WriteString(fmt.Sprintf("# TYPE %s %s\n", fam.Name, fam.Type))
		switch fam.Type {
		case "counter":
			for _, s := range fam.Samples {
				sb.WriteString(fmt.Sprintf("%s %g\n", fam.Name, s.Value))
			}
		case "histogram":
			for _, s := range fam.Samples {
				if le, ok := s.Labels["le"]; ok {
					sb.WriteString(fmt.Sprintf("%s_bucket{le=%q} %g\n", fam.Name, le, s.Value))
				} else if _, ok := s.Labels["__sum__"]; ok {
					sb.WriteString(fmt.Sprintf("%s_sum %g\n", fam.Name, s.Value))
				} else if _, ok := s.Labels["__count__"]; ok {
					sb.WriteString(fmt.Sprintf("%s_count %g\n", fam.Name, s.Value))
				}
			}
		}
	}
	return sb.String()
}

// ---- atomicCounter -------------------------------------------------------

type atomicCounter struct {
	bits uint64 // stores float64 bits
}

func (c *atomicCounter) Inc()          { c.Add(1) }
func (c *atomicCounter) Value() float64 { return math.Float64frombits(atomic.LoadUint64(&c.bits)) }

func (c *atomicCounter) Add(n float64) {
	for {
		old := atomic.LoadUint64(&c.bits)
		newVal := math.Float64frombits(old) + n
		if atomic.CompareAndSwapUint64(&c.bits, old, math.Float64bits(newVal)) {
			return
		}
	}
}

// ---- atomicHistogram -----------------------------------------------------

type atomicHistogram struct {
	buckets []float64 // upper bounds
	counts  []uint64  // per-bucket cumulative counts (len = len(buckets))
	total   uint64    // total count
	sumBits uint64    // float64 bits of sum
}

func newAtomicHistogram(buckets []float64) *atomicHistogram {
	sorted := make([]float64, len(buckets))
	copy(sorted, buckets)
	sort.Float64s(sorted)
	return &atomicHistogram{
		buckets: sorted,
		counts:  make([]uint64, len(sorted)),
	}
}

func (h *atomicHistogram) Observe(v float64) {
	for i, b := range h.buckets {
		if v <= b {
			atomic.AddUint64(&h.counts[i], 1)
		}
	}
	atomic.AddUint64(&h.total, 1)
	for {
		old := atomic.LoadUint64(&h.sumBits)
		newSum := math.Float64frombits(old) + v
		if atomic.CompareAndSwapUint64(&h.sumBits, old, math.Float64bits(newSum)) {
			return
		}
	}
}

func (h *atomicHistogram) Snapshot() HistogramSnapshot {
	snap := HistogramSnapshot{
		Count:   atomic.LoadUint64(&h.total),
		Sum:     math.Float64frombits(atomic.LoadUint64(&h.sumBits)),
		Buckets: make([]BucketCount, len(h.buckets)),
	}
	for i, b := range h.buckets {
		snap.Buckets[i] = BucketCount{
			UpperBound: b,
			Count:      atomic.LoadUint64(&h.counts[i]),
		}
	}
	return snap
}

// ---- helpers -------------------------------------------------------------

func formatFloat(f float64) string {
	if f == math.Inf(1) {
		return "+Inf"
	}
	return fmt.Sprintf("%g", f)
}
