package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/miekg/dns"
	"github.com/wcharczuk/go-chart"
)

var (
	domainlist = flag.String("d", "", "dns query wordlists file")
	client     = flag.String("client", "", "set edns client-subnet option")
	nameserver = flag.String("ns", "8.8.8.8", "set preferred nameserver")
	threads    = flag.Int("t", 100, "number of threads")
	verbose    = flag.Bool("v", false, "enable verbose output of dns queries (debug)")
)

type statistics struct {
	attempts int
	success  int
	fail     int
}

// Benchmarks for nameserver domain requests
var (
	Stats     statistics
	StartTime time.Time = time.Now()
	AvgRate   float64
)

var (
	rateValues = []float64{0}
	timeValues = []float64{0}
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] -ns {nameserver}\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()
	if *domainlist == "" {
		flag.Usage()
		os.Exit(1)
	}

	domains := make(chan string, *threads)
	results := make(chan string)
	done := make(chan bool)

	for i := 0; i < cap(domains); i++ {
		go makeRequest(domains, results)
	}

	qnames, err := getDomains()
	if err != nil {
		log.Fatalf("%v", err)
		os.Exit(1)
	}

	go func() {
		for _, q := range qnames {
			Stats.attempts++
			domains <- q
		}
	}()

	if !*verbose {
		go updateStats(done)
	}

	for i := 0; i < len(qnames); i++ {
		response := <-results

		if response != "err" {
			Stats.success++
			if *verbose {
				fmt.Printf("%v", response)
			}
		} else {
			Stats.fail++
		}
	}

	done <- true
	defer close(domains)
	defer close(results)
	defer close(done)
	finalStats()
}

func makeRequest(domains, results chan string) {
	for d := range domains {
		msg := buildQuery(dns.Id(), d, dns.TypeA, dns.ClassINET)

		r, err := dns.Exchange(msg, fmt.Sprintf("%v:53", *nameserver))
		if err != nil {
			results <- fmt.Sprintf("err")
			continue
		}
		results <- r.String()
	}

}

func getDomains() ([]string, error) {
	var qname []string

	if *domainlist == "" {
		return nil, fmt.Errorf("Domain file not provided")
	}

	f, err := os.Open(*domainlist)
	if err != nil {
		return nil, fmt.Errorf("Domain file not provided")
	}

	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		qname = append(qname, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
		return nil, fmt.Errorf("%v", err)
	}
	return qname, err
}

func updateStats(done chan bool) {
	ticker := time.NewTicker(500 * time.Millisecond)

	go func(done chan bool) {
		for {
			select {
			case <-done:
				return
			default:
				fmt.Printf("\033[2K\rRate: %v queries/sec", getStatAvg())
			}
		}
	}(done)

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			timeValues = append(timeValues, float64(time.Since(StartTime).Seconds()))
			rateValues = append(rateValues, getStatAvg())
		}

	}
}

func finalStats() {
	buildGraph(*nameserver, len(*client) != 0, timeValues, rateValues)

	fmt.Printf("\n\nStats\nAttempts: %v\nSuccess: %v\nFailed: %v\n\n"+
		"Avg Rate: %v queries/sec",
		Stats.attempts, Stats.success, Stats.fail, getStatAvg())
}

func getStatAvg() float64 {
	runTime := float64(time.Since(StartTime).Seconds())
	successCount := float64(Stats.success)
	successRate := successCount / runTime

	AvgRate += successRate

	return successRate
}

func buildQuery(id uint16, name string, qtype uint16, qclass uint16) *dns.Msg {
	m := &dns.Msg{
		MsgHdr: dns.MsgHdr{
			Authoritative:     false,
			AuthenticatedData: false,
			CheckingDisabled:  false,
			RecursionDesired:  true,
			Opcode:            dns.OpcodeQuery,
			Id:                id,
			Rcode:             dns.RcodeSuccess,
		},
		Question: make([]dns.Question, 1),
	}
	m.Question[0] = dns.Question{
		Name:   dns.Fqdn(name),
		Qtype:  qtype,
		Qclass: qclass,
	}

	if *client != "" {
		m.Extra = append(m.Extra, setupOptions())
	}
	return m
}

func setupOptions() *dns.OPT {
	o := &dns.OPT{
		Hdr: dns.RR_Header{
			Name:   ".",
			Rrtype: dns.TypeOPT,
		},
	}
	e := &dns.EDNS0_SUBNET{
		Code:          dns.EDNS0SUBNET,
		Address:       net.ParseIP(*client),
		Family:        1, // IP4
		SourceNetmask: net.IPv4len * 8,
	}
	o.Option = append(o.Option, e)

	return o
}

func buildGraph(nameserver string, clientStatus bool, t, c []float64) {
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
		Title: fmt.Sprintf("Nameserver:%v - SubnetClient: %v",
			nameserver, clientStatus),
		TitleStyle: chart.Style{
			FontSize: 14.0,
			Padding: chart.Box{
				Bottom: 30,
			},
		},
		Canvas: chart.Style{
			Padding: chart.Box{
				Top:    60,
				Bottom: 30,
				Left:   30,
				Right:  30,
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

	f, _ := os.Create(fmt.Sprintf("ns-%v_client-%v_%4v.png",
		nameserver, clientStatus, time.Now().Unix()))
	defer f.Close()
	graph.Render(chart.PNG, f)

}
