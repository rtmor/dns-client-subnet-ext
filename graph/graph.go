package graph

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/wcharczuk/go-chart"
)

// BuildGraph initializes new 2-axis graph
func BuildGraph(nameserver string, clientStatus bool, t, c []float64,
	threads, dmnCount int, output string) {
	mainSeries := chart.ContinuousSeries{
		Name:    "Rate",
		XValues: t,
		YValues: c,
	}

	// note we create a SimpleMovingAverage series by assignin the inner series.
	// we need to use a reference because `.Render()` needs to modify state within the series.
	smaSeries := &chart.SMASeries{
		Name:        "Average Rate",
		InnerSeries: mainSeries,
	} // we can optionally set the `WindowSize` property which alters how the moving average is calculated.

	graph := chart.Chart{
		Title: fmt.Sprintf("%v - +subnet_client: %v, +thread_count:%v, +domain_count:%v",
			nameserver, clientStatus, threads, dmnCount),
		TitleStyle: chart.Style{
			FontSize: 8.0,
			Padding: chart.Box{
				Top:    20,
				Bottom: 30,
				IsSet:  true,
			},
		},
		Height: 400,
		Width:  650,
		Canvas: chart.Style{
			Padding: chart.Box{
				Top:    60,
				Bottom: 30,
				Left:   30,
				Right:  30,
				IsSet:  true,
			},
		},
		XAxis: chart.XAxis{
			Name: "Elapsed Time (sec)",
			Range: &chart.ContinuousRange{
				Min: 0.0,
				Max: t[len(t)-1],
			},
		},
		YAxis: chart.YAxis{
			Name: "Query Return Rate/Sec",
			Range: &chart.ContinuousRange{
				Min: 0.0,
				// Max: c[len(c)-1],
			},
		},
		Series: []chart.Series{
			mainSeries,
			smaSeries,
			chart.ContinuousSeries{
				Style: chart.Style{
					StrokeColor: chart.GetDefaultColor(0).WithAlpha(64),
					FillColor:   chart.GetDefaultColor(0).WithAlpha(64),
				},
			},
		},
	}

	graph.Elements = []chart.Renderable{
		chart.Legend(&graph),
	}

	if _, err := os.Stat(output); os.IsNotExist(err) {
		os.Mkdir(output, os.ModePerm)
	}

	f, err := os.Create(fmt.Sprintf("%v/ns-%v_client-%v_%4v.png",
		output, nameserver, clientStatus, time.Now().Unix()))
	if err != nil {
		log.Printf("Error writing to file\n%v", err)
	}

	defer f.Close()
	graph.Render(chart.PNG, f)

}
