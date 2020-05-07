package statistics

import (
	"time"
)

type statistics struct {
	attempts int
	success  int
	fail     int
}

var (
	rateValues = []float64{0}
	timeValues = []float64{0}
)

var (
	sendingDelay time.Duration
	retryDelay   time.Duration
)

func getStatAvg() float64 {
	runTime := float64(time.Since(t0).Seconds())
	successCount := float64(stats.success)
	successRate := successCount / runTime

	avgRate += successRate

	return successRate
}

func updateStats(done <-chan bool) {
	var deltaCount int
	var deadStop int = 75
	interval := 50 * time.Millisecond
	ticker := time.NewTicker(interval)
	lastCount := stats.success

	for {
		select {
		case <-done:
			ticker.Stop()
			return
		case <-ticker.C:
			if deltaCount == 0 {
				deadStop--
				if deadStop < 1 {
					graph.BuildGraph(*dnsServer, *client, len(*client) != 0,
						&timeValues, &rateValues, *concurrency, stats.success, *outputDir)
					fmt.Println("Requests being decline. Terminating query.")
					os.Exit(2)
				}
			}
			currentCount := stats.success
			deltaCount = currentCount - lastCount
			lastCount = currentCount
			timeValues = append(timeValues, float64(time.Since(t0).Seconds()))
			rateValues = append(rateValues,
				float64(deltaCount)/float64(interval)*float64(time.Second))
		default:
			fmt.Printf("\033[2K\r[%.2f] rate: %.4f queries/s",
				float64(time.Since(t0).Seconds()),
				float64(deltaCount)/float64(interval)*float64(time.Second))
		}

	}
}
