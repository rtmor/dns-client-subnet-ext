package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"time"

	"github.com/miekg/dns"
	"github.com/rtmoranorg/dns-client-subnet-ext/graph"
)

type domainRecord struct {
	id      uint16
	domain  string
	timeout time.Time
	resend  int
}

type domainAnswer struct {
	id     uint16
	domain string
	ips    []net.IP
}

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

var (
	t0      time.Time = time.Now()
	td      time.Duration
	avgRate float64
	stats   statistics
)

var (
	dnsServer        = flag.String("ns", "8.8.8.8", "DNS server address (ip)")
	concurrency      = flag.Int("t", 1000, "Number of concurrent workers")
	packetsPerSecond = flag.Int("pps", 2000, "Send up to PPS DNS queries per second")
	retryTime        = flag.String("rr", "1s", "Resend unanswered query after RETRY")
	verbose          = flag.Bool("v", false, "Verbose logging")
	domainList       = flag.String("d", "", "Location of domain list file")
	client           = flag.String("c", "", "Client subnet address")
	outputDir        = flag.String("o", "output", "Location of output directory")
	retryCount       = flag.Int("retries", 1, "Number of attempts made to resolve a domain")
)

func main() {
	domains := make(chan string, *concurrency)
	domainSlotAvailable := make(chan bool, *concurrency)

	for i := 0; i < *concurrency; i++ {
		domainSlotAvailable <- true
	}

	go readDomains(domains, domainSlotAvailable)

	c, err := net.Dial("udp", fmt.Sprintf("%v:53", *dnsServer))
	if err != nil {
		fmt.Fprintf(os.Stderr, "bind(udp, %s): %s\n", *dnsServer, err)
		os.Exit(1)
	}

	// Used as a queue. Make sure it has plenty of storage available.
	timeoutRegister := make(chan *domainRecord, *concurrency*1000)
	timeoutExpired := make(chan *domainRecord)

	resolved := make(chan *domainAnswer, *concurrency)
	tryResolving := make(chan *domainRecord, *concurrency)

	done := make(chan bool)

	go getTimeout(timeoutRegister, timeoutExpired)
	go writeRequest(c, tryResolving)
	go readRequest(c, resolved)
	go updateStats(done)

	t0 = time.Now()

	avgTries := doMapGuard(
		domains, domainSlotAvailable,
		timeoutRegister, timeoutExpired,
		tryResolving, resolved)

	td = time.Now().Sub(t0)
	done <- true

	finalStats(avgTries)
}

func doMapGuard(
	domains <-chan string,
	domainSlotAvailable chan<- bool,
	timeoutRegister chan<- *domainRecord,
	timeoutExpired <-chan *domainRecord,
	tryResolving chan<- *domainRecord,
	resolved <-chan *domainAnswer) float64 {

	m := make(map[uint16]*domainRecord)

	done := false

	sumTries := 0

	for done == false || len(m) > 0 {
		select {
		case domain := <-domains:
			if domain == "" {
				domains = make(chan string)
				done = true
				break
			}

			var id uint16
			for {
				id = dns.Id()
				if id != 0 && m[id] == nil {
					break
				}
			}
			stats.attempts++

			dr := &domainRecord{id, domain, time.Now(), 1}
			m[id] = dr

			if *verbose {
				fmt.Fprintf(os.Stderr, "0x%04x resolving %s\n", id, domain)
			}

			timeoutRegister <- dr
			tryResolving <- dr

		case dr := <-timeoutExpired:
			if m[dr.id] == dr {
				if dr.resend == *retryCount {
					delete(m, dr.id)
					stats.fail++

					if *verbose {
						fmt.Fprintf(os.Stderr, "0x%04x resend (FAILED: exceed 3 attempts) %s\n", dr.id,
							dr.domain)
					}
					continue
				}
				dr.resend++
				dr.timeout = time.Now()
				if *verbose {
					fmt.Fprintf(os.Stderr, "0x%04x resend (try:%d) %s\n", dr.id,
						dr.resend, dr.domain)
				}
				timeoutRegister <- dr
				tryResolving <- dr
			}

		case da := <-resolved:
			if m[da.id] != nil {
				dr := m[da.id]
				if dr.domain != da.domain {
					if *verbose {
						fmt.Fprintf(os.Stderr, "0x%04x error, unrecognized domain: %s != %s\n",
							da.id, dr.domain, da.domain)
					}
					break
				}

				if *verbose {
					fmt.Fprintf(os.Stderr, "0x%04x resolved %s\n",
						dr.id, dr.domain)
				}

				s := make([]string, 0, 16)
				for _, ip := range da.ips {
					s = append(s, ip.String())
				}
				sort.Sort(sort.StringSlice(s))

				sumTries += dr.resend
				stats.success++

				delete(m, dr.id)
				domainSlotAvailable <- true
			}
		}
	}
	return float64(sumTries) / float64(stats.success)
}

func getTimeout(timeoutRegister <-chan *domainRecord,
	timeoutExpired chan<- *domainRecord) {
	for {
		dr := <-timeoutRegister
		t := dr.timeout.Add(retryDelay)
		now := time.Now()
		if t.Sub(now) > 0 {
			delta := t.Sub(now)
			time.Sleep(delta)
		}
		timeoutExpired <- dr
	}
}

func writeRequest(c net.Conn, tryResolving <-chan *domainRecord) {
	for {
		dr := <-tryResolving

		t := dns.TypeA
		msg := buildQuery(dr.id, dr.domain, t, dns.ClassINET)

		_, err := c.Write(msg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "write(udp): %s\n", err)
			os.Exit(1)
		}
		time.Sleep(sendingDelay)
	}
}

func readRequest(c net.Conn, resolved chan<- *domainAnswer) {
	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			os.Exit(1)
		}

		msg := new(dns.Msg)
		msg.Unpack(buf[:n])

		domain := msg.Question[0].Name
		id := msg.Id
		var ips []net.IP

		for _, a := range msg.Answer {
			if t, ok := a.(*dns.A); ok {
				ips = append(ips, t.A.To4())
			}
		}
		resolved <- &domainAnswer{id, domain, ips}
	}
}

func buildQuery(id uint16, name string, qtype uint16, qclass uint16) []byte {
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

	msg, _ := m.Pack()
	return msg
}

func setupOptions() *dns.OPT {
	o := &dns.OPT{
		Hdr: dns.RR_Header{
			Name:   ".",
			Rrtype: dns.TypeOPT,
		},
	}
	e := &dns.EDNS0_SUBNET{
		Code:    dns.EDNS0SUBNET,
		Address: net.ParseIP(*client).To4(),
		Family:  1, // IP4
		// SourceNetmask: net.IPv4len * 8,
		SourceNetmask: 0,
		SourceScope:   0,
	}
	o.Option = append(o.Option, e)

	return o
}

func getStatAvg() float64 {
	runTime := float64(time.Since(t0).Seconds())
	successCount := float64(stats.success)
	successRate := successCount / runTime

	avgRate += successRate

	return successRate
}

// GetDomains returns string slice of domains within specified file
func GetDomains(n string) ([]string, error) {
	var qname []string

	if n == "" {
		return nil, fmt.Errorf("Domain file not provided")
	}

	f, err := os.Open(n)
	if err != nil {
		return nil, fmt.Errorf("Failed to open domain file")
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

func readDomains(domains chan<- string, domainSlotAvailable <-chan bool) {
	i := 0
	in, err := GetDomains(*domainList)
	domainLength := len(in)
	if err != nil {
		fmt.Printf("%v", err)
	}

	for range domainSlotAvailable {

		if i == domainLength-1 {
			break
		}

		domain := in[i] + "."

		domains <- domain
		i++
	}
	close(domains)
}

func updateStats(done <-chan bool) {
	ticker := time.NewTicker(100 * time.Millisecond)
	lastCount := stats.success
	var deltaCount int
	var deadStop int = 75

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
					fmt.Println("Requests being decline. Terminating Query.")
					os.Exit(2)
				}
			}
			currentCount := stats.success
			deltaCount = currentCount - lastCount
			lastCount = currentCount
			timeValues = append(timeValues, float64(time.Since(t0).Seconds()))
			rateValues = append(rateValues, float64(deltaCount*10))
		default:
			fmt.Printf("\033[2K\rRate: %.4f queries/s", float64(deltaCount*10))
		}

	}
}

func finalStats(avgTries float64) {
	graph.BuildGraph(*dnsServer, *client, len(*client) != 0,
		&timeValues, &rateValues, *concurrency, stats.success, *outputDir)

	fmt.Printf("\n\nFinal Statistics\n"+
		"[+] Attempts:         %v\n"+
		"[+] Success:          %v\n"+
		"[+] Failed:           %v\n"+
		"[+] Avg Retry Count:  %.3f\n"+
		"[+] Avg Rate:         %.3f queries/s\n"+
		"[+] Elapsed Time:     %.3f s\n",
		stats.attempts, stats.success, stats.fail,
		avgTries, getStatAvg(), td.Seconds())
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] -ns {nameserver}\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *domainList == "" {
		fmt.Println("Missing required domain list")
		flag.Usage()
		os.Exit(1)
	}

	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(1)
	}

	sendingDelay = time.Duration(1000000000/(*packetsPerSecond)) * time.Nanosecond
	var err error
	retryDelay, err = time.ParseDuration(*retryTime)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't parse duration %s\n", *retryTime)
		os.Exit(1)
	}

	var clientSub string
	if *client == "" {
		clientSub = "disabled"
	} else {
		clientSub = *client
	}

	getBanner(sendingDelay, retryDelay, clientSub)
}

func getBanner(sndDelay, retryDelay time.Duration, client string) {
	fmt.Printf("DNS Resolver Subnet Client Test\n"+
		"[+] Nameserver:    %v\n"+
		"[+] Subnet Client: %v\n"+
		"[+] Thread Count:  %v\n"+
		"[+] Sending Delay: %s (%d pps)\n"+
		"[+] Retry Delay:   %s\n\n",
		*dnsServer, client, *concurrency, sendingDelay,
		*packetsPerSecond, retryDelay)
}
