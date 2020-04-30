package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/miekg/dns"
)

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
	in, err := GetDomains(domainList)
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

var (
	sendingDelay time.Duration
	retryDelay time.Duration
)

var (
	concurrency int
	dnsServer string
	packetsPerSecond int
	retryTime string
	verbose bool
	domainList string
	client string
)

func init() {
	flag.StringVar(&dnsServer, "server", "8.8.8.8:53",
		"DNS server address (ip:port)")
	flag.IntVar(&concurrency, "concurrency", 1000,
		"Internal buffer")
	flag.IntVar(&packetsPerSecond, "pps", 2000,
		"Send up to PPS DNS queries per second")
	flag.StringVar(&retryTime, "retry", "1s",
		"Resend unanswered query after RETRY")
	flag.BoolVar(&verbose, "v", false,
		"Verbose logging")
	flag.BoolVar(&ipv6, "6", false,
		"Ipv6 - ask for AAAA, not A")
	flag.StringVar(&domainList, "d", "", "location of domain list file")
	flag.StringVar(&client, "c", "", "client subnet address")
}

func main() {
	checkFlags()

	sendingDelay = time.Duration(1000000000/packetsPerSecond) * time.Nanosecond
	var err error
	retryDelay, err = time.ParseDuration(retryTime)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't parse duration %s\n", retryTime)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Server: %s, sending delay: %s (%d pps), retry delay: %s\n",
		dnsServer, sendingDelay, packetsPerSecond, retryDelay)

	domains := make(chan string, concurrency)
	domainSlotAvailable := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		domainSlotAvailable <- true
	}

	go readDomains(domains, domainSlotAvailable)

	c, err := net.Dial("udp", dnsServer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bind(udp, %s): %s\n", dnsServer, err)
		os.Exit(1)
	}

	// Used as a queue. Make sure it has plenty of storage available.
	timeoutRegister := make(chan *domainRecord, concurrency*1000)
	timeoutExpired := make(chan *domainRecord)

	resolved := make(chan *domainAnswer, concurrency)
	tryResolving := make(chan *domainRecord, concurrency)

	go doTimeouter(timeoutRegister, timeoutExpired)

	go writeRequest(c, tryResolving)
	go readRequest(c, resolved)

	t0 := time.Now()
	domainsCount, avgTries := doMapGuard(domains, domainSlotAvailable,
		timeoutRegister, timeoutExpired,
		tryResolving, resolved)
	td := time.Now().Sub(t0)
	fmt.Fprintf(os.Stderr, "Resolved %d domains in %.3fs. Average retries %.3f. Domains per second: %.3f\n",
		domainsCount,
		td.Seconds(),
		avgTries,
		float64(domainsCount)/td.Seconds())
}

func checkFlags() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, strings.Join([]string{
			"\"resolve\" mass resolve DNS A records for domains names read from stdin.",
			"",
			"Usage: resolve [option ...]",
			"",
		}, "\n"))
		flag.PrintDefaults()
	}

	if domainList == "" {
		fmt.Println("Missing required domain list")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(1)
	}
}

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

func doMapGuard(domains <-chan string,
	domainSlotAvailable chan<- bool,
	timeoutRegister chan<- *domainRecord,
	timeoutExpired <-chan *domainRecord,
	tryResolving chan<- *domainRecord,
	resolved <-chan *domainAnswer) (int, float64) {

	m := make(map[uint16]*domainRecord)

	done := false

	sumTries := 0
	domainCount := 0

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
				id = uint16(rand.Int())
				if id != 0 && m[id] == nil {
					break
				}
			}
			dr := &domainRecord{id, domain, time.Now(), 1}
			m[id] = dr
			if verbose {
				fmt.Fprintf(os.Stderr, "0x%04x resolving %s\n", id, domain)
			}
			timeoutRegister <- dr
			tryResolving <- dr

		case dr := <-timeoutExpired:
			if m[dr.id] == dr {
				dr.resend++
				dr.timeout = time.Now()
				if verbose {
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
					if verbose {
						fmt.Fprintf(os.Stderr, "0x%04x error, unrecognized domain: %s != %s\n",
							da.id, dr.domain, da.domain)
					}
					break
				}

				if verbose {
					fmt.Fprintf(os.Stderr, "0x%04x resolved %s\n",
						dr.id, dr.domain)
				}

				s := make([]string, 0, 16)
				for _, ip := range da.ips {
					s = append(s, ip.String())
				}
				sort.Sort(sort.StringSlice(s))

				// without trailing dot
				domain := dr.domain[:len(dr.domain)-1]
				fmt.Printf("%s, %s\n", domain, strings.Join(s, " "))

				sumTries += dr.resend
				domainCount++

				delete(m, dr.id)
				domainSlotAvailable <- true
			}
		}
	}
	return domainCount, float64(sumTries) / float64(domainCount)
}

func doTimeouter(timeoutRegister <-chan *domainRecord,
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

		var t uint16
		if !ipv6 {
			t = dns.TypeA
		} else {
			t = dns.TypeAAAA
		}
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

	if client != "" {
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
		Address: net.ParseIP(client).To4(),
		Family:  1, // IP4
		// SourceNetmask: net.IPv4len * 8,
		SourceNetmask: 0,
		SourceScope:   0,
	}
	o.Option = append(o.Option, e)

	return o
}

// func unpackDNS(msg []byte, dnsType uint16) (domain string, id uint16, ips []net.IP) {
// 	d := new(dns.Msg)
// 	if err := d.Unpack(msg); err != nil {
// 		// fmt.Fprintf(os.Stderr, "dns error (unpacking)\n")
// 		return
// 	}

// 	id = d.Id
// 	// id = d.id

// 	if len(d.Question) < 1 {
// 		// fmt.Fprintf(os.Stderr, "dns error (wrong question section)\n")
// 		return
// 	}

// 	domain = d.Question[0].Name
// 	if len(domain) < 1 {
// 		// fmt.Fprintf(os.Stderr, "dns error (wrong domain in question)\n")
// 		return
// 	}

// 	_, addrs, err := answer(domain, "server", d, dnsType)
// 	if err == nil {
// 		switch dnsType {
// 		case dnsTypeA:
// 			ips = convertRR_A(addrs)
// 		case dnsTypeAAAA:
// 			ips = convertRR_AAAA(addrs)
// 		}
// 	}
// 	return
// }
