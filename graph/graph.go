package graph

import (
	"fmt"
	"os"
	"time"

	"github.com/wcharczuk/go-chart"
)

// BuildGraph outputs dns query data per second
func BuildGraph(nameserver string, clientStatus bool, t, c []float64) {

	graph := chart.Chart{
		Title: fmt.Sprintf("DNS Requests/sec\nNameserver:%v\nSubnetClient: %v",
			nameserver, clientStatus),
		XAxis: chart.XAxis{
			Name: "Elapsed Time (sec)",
		},
		YAxis: chart.YAxis{
			Name: "Returned DNS Requests",
		},
		Series: []chart.Series{
			chart.ContinuousSeries{
				Style: chart.Style{
					StrokeColor: chart.GetDefaultColor(0).WithAlpha(64),
					FillColor:   chart.GetDefaultColor(0).WithAlpha(64),
				},
				XValues: t,
				YValues: c,
			},
		},
	}

	f, _ := os.Create(fmt.Sprintf("%v_client-%v_%.6v",
		nameserver, clientStatus, time.Now()))
	defer f.Close()
	graph.Render(chart.PNG, f)
}
