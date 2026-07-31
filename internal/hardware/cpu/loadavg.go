package cpu

import "math"

// The load average, as close as Windows allows.
//
// Linux averages the number of threads that are running or waiting to run.
// Windows has no equivalent counter to read, so this measures the same thing
// from the other side: utilisation times the number of logical processors,
// which is how many processors' worth of work is actually being done. A load
// of 4 on a sixteen thread machine means four threads' worth, exactly as it
// would on Linux — what it cannot show is a queue longer than the machine is
// wide, because a saturated processor caps at its own core count.
//
// The smoothing constants are the ones Linux uses, so the three numbers decay
// at the familiar rate.

// loadWindows are the averaging periods, in seconds.
var loadWindows = [3]float64{60, 300, 900}

// LoadAverage keeps the three exponentially weighted averages.
//
// It is sampled at whatever rate the collector runs, so the decay is computed
// from the elapsed time rather than assumed: changing the read interval must
// not change what a one minute average means.
type LoadAverage struct {
	values [3]float64
	// primed is false until the first sample, which is taken as the starting
	// value rather than decayed towards from zero. Otherwise every start
	// would show a minute of artificially low load.
	primed bool
}

// Add folds one sample in. load is processor-equivalents busy, elapsed is the
// time since the previous sample.
func (l *LoadAverage) Add(load, elapsedSeconds float64) {
	if math.IsNaN(load) || math.IsInf(load, 0) || load < 0 {
		return
	}
	if !l.primed {
		l.values = [3]float64{load, load, load}
		l.primed = true
		return
	}
	if elapsedSeconds <= 0 {
		return
	}

	for i, window := range loadWindows {
		// The weight of the existing average over this interval. Sampling
		// twice as often halves each step, and the average still describes
		// the same span of time.
		decay := math.Exp(-elapsedSeconds / window)
		l.values[i] = l.values[i]*decay + load*(1-decay)
	}
}

// Values returns the one, five and fifteen minute averages, and whether any
// sample has been taken yet.
func (l *LoadAverage) Values() ([3]float64, bool) {
	return l.values, l.primed
}
