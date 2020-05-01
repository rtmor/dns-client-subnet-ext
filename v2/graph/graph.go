package graph

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/wcharczuk/go-chart"
)

// BuildGraph initializes new 2-axis graph
func BuildGraph(nameserver, client string, clientStatus bool,
	t, c *[]float64, threads, dmnCount int, output string) {
	mainSeries := chart.ContinuousSeries{
		Name:    "Rate",
		XValues: *t,
		YValues: *c,
	}

	smaSeries := &chart.SMASeries{
		Name:        "Average Rate",
		InnerSeries: mainSeries,
	}

	graph := chart.Chart{
		Title: fmt.Sprintf("ns:%v - subnet_client: %v %v | thread_count:%v | domain_count:%v",
			nameserver, clientStatus, client, threads, dmnCount),
		TitleStyle: chart.Style{
			FontSize: 8.0,
			Padding: chart.Box{
				Top:    20,
				Bottom: 30,
				Left:   95,
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
			},
		},
		YAxis: chart.YAxis{
			Name: "Successful Queries/s",
			Range: &chart.ContinuousRange{
				Min: 0.0,
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

	newpath := filepath.Join(".", output, nameserver)
	os.MkdirAll(newpath, os.ModePerm)

	f, err := os.Create(fmt.Sprintf("%v/%v/ns-%v_client-%v_%4v.png",
		output, nameserver, nameserver, clientStatus, time.Now().Unix()))
	if err != nil {
		log.Printf("Error writing to file\n%v", err)
	}

	defer f.Close()
	graph.Render(chart.PNG, f)
}
