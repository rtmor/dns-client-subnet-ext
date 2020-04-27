package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/miekg/dns"
	"github.com/rtmoranorg/dns-client-subnet-ext/domain"
	"github.com/rtmoranorg/dns-client-subnet-ext/graph"
)

var (
	domainlist = flag.String("d", "", "dns query wordlists file")
	client     = flag.String("client", "", "set edns client-subnet option")
	nameserver = flag.String("ns", "8.8.8.8", "set preferred nameserver")
	threads    = flag.Int("t", 100, "number of threads")
	verbose    = flag.Bool("v", false, "enable verbose output of dns queries (debug)")
	output     = flag.String("o", "data", "output directory for data graph")
)

type statistics struct {
	attempts int
	success  int
	fail     int
}

// Benchmarks for nameserver domain requests
var (
	stats       statistics
	startTime   time.Time = time.Now()
	avgRate     float64
	domainCount int
)

var (
	rateValues = []float64{0}
	timeValues = []float64{0}
)

func main() {
	checkFlags()

	domains := make(chan string, *threads)
	results := make(chan string)
	done := make(chan bool)

	for i := 0; i < cap(domains); i++ {
		go makeRequest(domains, results)
	}

	qnames, err := domain.GetDomains(*domainlist)
	if err != nil {
		log.Fatalf("%v", err)
		os.Exit(1)
	}
	domainCount = len(qnames)

	go func() {
		for _, q := range qnames {
			stats.attempts++
			domains <- q
		}
	}()

	if !*verbose {
		go updateStats(done)
	}

	for i := 0; i < len(qnames); i++ {
		response := <-results

		if response != "err" {
			stats.success++
			if *verbose {
				fmt.Printf("%v", response)
			}
		} else {
			stats.fail++
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
			results <- "err"
			continue
		}
		results <- r.String()
	}

}

func updateStats(done chan bool) {
	ticker := time.NewTicker(500 * time.Millisecond)

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			timeValues = append(timeValues, float64(time.Since(startTime).Seconds()))
			rateValues = append(rateValues, getStatAvg())
		default:
			fmt.Printf("\033[2K\rRate: %.4f queries/s", getStatAvg())
		}

	}
}

func finalStats() {
	graph.BuildGraph(*nameserver, *client, len(*client) != 0,
		&timeValues, &rateValues, *threads, domainCount, *output)

	fmt.Printf("\n\nFinal Statistics\n"+
		"[+] Attempts:      %v\n"+
		"[+] Success:       %v\n"+
		"[+] Failed:        %v\n"+
		"[+] Avg Rate:      %.4f queries/s\n"+
		"[+] Elapsed Time:  %.4f seconds",
		stats.attempts, stats.success, stats.fail,
		getStatAvg(), float64(time.Since(startTime).Seconds()))
}

func getStatAvg() float64 {
	runTime := float64(time.Since(startTime).Seconds())
	successCount := float64(stats.success)
	successRate := successCount / runTime

	avgRate += successRate

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

func checkFlags() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] -ns {nameserver}\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()
	if *domainlist == "" {
		flag.Usage()
		os.Exit(1)
	}

	getBanner()
}

func getBanner() {
	fmt.Printf("DNS Resolver Subnet Client Test\n"+
		"[+] Nameserver:    %v\n"+
		"[+] Subnet Client: %v\n"+
		"[+] Thread Count:  %v\n\n",
		*nameserver, *client, *threads)
}
