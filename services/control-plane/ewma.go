package controlplane

import (
	"math"
	"sync"
	"time"
)

const (
	defaultEwmaAlpha   = 0.2
	defaultEwmaEpsilon = 1e-9
)

type EwmaConfig struct {
	// Weight given to each new sample. Higher reacts faster and smooths less.
	Alpha float64
	// Floor under the variance so Stddev never divides by or returns zero.
	Epsilon float64
}

type Ewma struct {
	mu sync.Mutex

	config EwmaConfig

	mean     float64
	variance float64
	n        int64
	lastSeen time.Time
}

type EwmaSnapshot struct {
	Mean     float64
	Variance float64
	Stddev   float64
	N        int64
	LastSeen time.Time
}

func NewEwma(config EwmaConfig) *Ewma {
	if config.Alpha <= 0 || config.Alpha > 1 {
		config.Alpha = defaultEwmaAlpha
	}

	if config.Epsilon <= 0 {
		config.Epsilon = defaultEwmaEpsilon
	}

	return &Ewma{config: config}
}

// Observe folds one sample into the average. The first sample seeds the mean
// outright rather than pulling a zero baseline toward it.
func (e *Ewma) Observe(ts time.Time, value float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.n == 0 {
		e.mean = value
		e.variance = 0
		e.n = 1
		e.lastSeen = ts

		return
	}

	delta := value - e.mean
	e.mean += e.config.Alpha * delta
	e.variance = (1 - e.config.Alpha) * (e.variance + e.config.Alpha*delta*delta)

	e.n++
	e.lastSeen = ts
}

// Snapshot reads every field under one lock, so Mean and Stddev always
// describe the same sample.
func (e *Ewma) Snapshot() EwmaSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	return EwmaSnapshot{
		Mean:     e.mean,
		Variance: e.variance,
		Stddev:   math.Sqrt(e.variance + e.config.Epsilon),
		N:        e.n,
		LastSeen: e.lastSeen,
	}
}

func (e *Ewma) Mean() float64 {
	return e.Snapshot().Mean
}

func (e *Ewma) Stddev() float64 {
	return e.Snapshot().Stddev
}
