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

var (
	stats     statistics
	startTime time.Time = time.Now()
	avgRate   float64
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
			stats.attempts++
			domains <- q
		}
	}()

	if !*verbose {
		go updateStats()
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

	close(domains)
	close(results)
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

func updateStats() {
	for {
		fmt.Printf("\033[2K\rRate: %v queries/sec", getStatAvg())
	}
}

func finalStats() {
	fmt.Printf("\n\nStats\nAttempts: %v\nSuccess: %v\nFailed: %v\n\n"+
		"Avg Rate: %v queries/sec",
		stats.attempts, stats.success, stats.fail, getStatAvg())
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
