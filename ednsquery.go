// EDNS_Client Subnet Test - rtmoran
// comprised of code miekg/exdns & amass/resolver/msg

package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
)

var (
	dnskey       *dns.DNSKEY
	short        = flag.Bool("short", false, "abbreviate long DNSSEC records")
	dnssec       = flag.Bool("dnssec", false, "request DNSSEC records")
	query        = flag.Bool("question", false, "show question")
	check        = flag.Bool("check", false, "check internal DNSSEC consistency")
	six          = flag.Bool("6", false, "use IPv6 only")
	four         = flag.Bool("4", false, "use IPv4 only")
	anchor       = flag.String("anchor", "", "use the DNSKEY in this file as trust anchor")
	tsig         = flag.String("tsig", "", "request tsig with key: [hmac:]name:key")
	port         = flag.Int("port", 53, "port number to use")
	aa           = flag.Bool("aa", false, "set AA flag in query")
	ad           = flag.Bool("ad", false, "set AD flag in query")
	cd           = flag.Bool("cd", false, "set CD flag in query")
	rd           = flag.Bool("rd", true, "set RD flag in query")
	fallback     = flag.Bool("fallback", false, "fallback to 4096 bytes bufsize and after that TCP")
	timeoutDial  = flag.Duration("timeout-dial", 2*time.Second, "Dial timeout")
	timeoutRead  = flag.Duration("timeout-read", 2*time.Second, "Read timeout")
	timeoutWrite = flag.Duration("timeout-write", 2*time.Second, "Write timeout")
	nsid         = flag.Bool("nsid", false, "set edns nsid option")
	client       = flag.String("client", "", "set edns client-subnet option")
	opcode       = flag.String("opcode", "query", "set opcode to query|update|notify")
	rcode        = flag.String("rcode", "success", "set rcode to noerror|formerr|nxdomain|servfail|...")
	domains      = flag.String("d", "", "dns query wordlists file")
)

type stats struct {
	attempts int
	success  int
	failed   int
}

type request struct {
	message dns.Msg
}

var (
	startTime time.Time = time.Now()
	avgRate   float64
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [@server] [qtype...] [qclass...] [name ...]\n", os.Args[0])
		flag.PrintDefaults()
	}

	var (
		qtype  []uint16
		qclass []uint16
		qname  []string
	)

	flag.Parse()
	if *anchor != "" {
		f, err := os.Open(*anchor)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failure to open %s: %s\n", *anchor, err.Error())
		}
		r, err := dns.ReadRR(f, *anchor)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failure to read an RR from %s: %s\n", *anchor, err.Error())
		}
		if k, ok := r.(*dns.DNSKEY); !ok {
			fmt.Fprintf(os.Stderr, "No DNSKEY read from %s\n", *anchor)
		} else {
			dnskey = k
		}
	}

	var nameserver string
	for _, arg := range flag.Args() {
		// If it starts with @ it is a nameserver
		if arg[0] == '@' {
			nameserver = arg
			continue
		}
		// First class, then type, to make ANY queries possible
		// And if it looks like type, it is a type
		if k, ok := dns.StringToType[strings.ToUpper(arg)]; ok {
			qtype = append(qtype, k)
			continue
		}
		// If it looks like a class, it is a class
		if k, ok := dns.StringToClass[strings.ToUpper(arg)]; ok {
			qclass = append(qclass, k)
			continue
		}
		// If it starts with TYPExxx it is unknown rr
		if strings.HasPrefix(arg, "TYPE") {
			i, err := strconv.Atoi(arg[4:])
			if err == nil {
				qtype = append(qtype, uint16(i))
				continue
			}
		}
		// If it starts with CLASSxxx it is unknown class
		if strings.HasPrefix(arg, "CLASS") {
			i, err := strconv.Atoi(arg[5:])
			if err == nil {
				qclass = append(qclass, uint16(i))
				continue
			}
		}
		// Anything else is a qname
		// qname = append(qname, arg)

	}

	if *domains != "" {
		file, err := os.Open(*domains)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			qname = append(qname, scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
	}
	if len(qname) == 0 {
		qname = []string{"."}
		if len(qtype) == 0 {
			qtype = append(qtype, dns.TypeNS)
		}
	}
	if len(qtype) == 0 {
		qtype = append(qtype, dns.TypeA)
	}
	if len(qclass) == 0 {
		qclass = append(qclass, dns.ClassINET)
	}

	if len(nameserver) == 0 {
		conf, err := dns.ClientConfigFromFile("/etc/resolv.conf")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		nameserver = "@" + conf.Servers[0]
	}

	nameserver = string([]byte(nameserver)[1:]) // chop off @
	// if the nameserver is from /etc/resolv.conf the [ and ] are already
	// added, thereby breaking net.ParseIP. Check for this and don't
	// fully qualify such a name
	if nameserver[0] == '[' && nameserver[len(nameserver)-1] == ']' {
		nameserver = nameserver[1 : len(nameserver)-1]
	}
	if i := net.ParseIP(nameserver); i != nil {
		nameserver = net.JoinHostPort(nameserver, strconv.Itoa(*port))
	} else {
		nameserver = dns.Fqdn(nameserver) + ":" + strconv.Itoa(*port)
	}

	c := new(dns.Client)
	t := new(dns.Transfer)

	c.Net = "udp"
	if *four {
		c.Net = "udp4"
	}
	if *six {
		c.Net = "udp6"
	}

	c.DialTimeout = *timeoutDial
	c.ReadTimeout = *timeoutRead
	c.WriteTimeout = *timeoutWrite

	qt := dns.TypeA
	qc := uint16(dns.ClassINET)

	stat := new(stats)

Query:
	for i, v := range qname {
		// go func(i int, v string) {

		if i < len(qtype) {
			qt = qtype[i]
		}
		if i < len(qclass) {
			qc = qclass[i]
		}

		m := queryMessage(dns.Id(), v, qt, qc)

		if *tsig != "" {
			if algo, name, secret, ok := tsigKeyParse(*tsig); ok {
				m.SetTsig(name, algo, 300, time.Now().Unix())
				c.TsigSecret = map[string]string{name: secret}
				t.TsigSecret = map[string]string{name: secret}
			} else {
				fmt.Fprintf(os.Stderr, "TSIG key data error\n")
				continue
			}
		}
		if *query {
			fmt.Printf("%s", m.String())
			fmt.Printf("\n;; size: %d bytes\n\n", m.Len())
		}
		if qt == dns.TypeAXFR || qt == dns.TypeIXFR {
			env, err := t.In(m, nameserver)
			if err != nil {
				fmt.Printf(";; %s\n", err.Error())
				continue
			}
			var envelope, record int
			for e := range env {
				if e.Error != nil {
					fmt.Printf(";; %s\n", e.Error.Error())
					continue Query
				}
				for _, r := range e.RR {
					fmt.Printf("%s\n", r)
				}
				record += len(e.RR)
				envelope++
			}
			fmt.Printf("\n;; xfr size: %d records (envelopes %d)\n", record, envelope)
			continue
		}

		r, _, err := c.Exchange(m, nameserver)
		stat.attempts++
		fmt.Printf("\033[2K\rRate: %v queries/sec", getStats(stat))
	Redo:
		switch err {
		case nil:
			stat.success++
		default:
			// fmt.Printf(";; %s\n", err.Error())
			stat.failed++
			continue
		}
		if r.Truncated {
			if *fallback {
				if !*dnssec {
					fmt.Printf(";; Truncated, trying %d bytes bufsize\n", dns.DefaultMsgSize)
					o := new(dns.OPT)
					o.Hdr.Name = "."
					o.Hdr.Rrtype = dns.TypeOPT
					o.SetUDPSize(dns.DefaultMsgSize)
					m.Extra = append(m.Extra, o)
					r, _, err = c.Exchange(m, nameserver)
					*dnssec = true
					goto Redo
				} else {
					fmt.Printf(";; Truncated, trying TCP\n")
					c.Net = "tcp"
					r, _, err = c.Exchange(m, nameserver)
					*fallback = false
					goto Redo
				}
			}
			fmt.Printf(";; Truncated\n")
		}
		if r.Id != m.Id {
			fmt.Fprintf(os.Stderr, "Id mismatch\n")
			return
		}

		if *short {
			shortenMsg(r)
		}

	}

	fmt.Printf("\n\nStats\nAttempts: %v\nSuccess: %v\nFailed: %v\n\n"+
		"Avg Rate: %v queries/sec",
		stat.attempts, stat.success, stat.failed, getStatAvg(stat))
}

func queryMessage(id uint16, name string, qtype uint16, qclass uint16) *dns.Msg {
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
	e := &dns.EDNS0_SUBNET{
		Code:          dns.EDNS0SUBNET,
		Family:        1,
		SourceNetmask: 0,
		SourceScope:   0,
		Address:       net.ParseIP(*client),
	}

	return &dns.OPT{
		Hdr: dns.RR_Header{
			Name:   ".",
			Rrtype: dns.TypeOPT,
		},
		Option: []dns.EDNS0{e},
	}
}

func getStats(stat *stats) float64 {

	runTime := float64(time.Since(startTime).Seconds())
	successCount := float64(stat.success)
	successRate := successCount / runTime

	avgRate += successRate

	return successRate
}

func getStatAvg(stat *stats) float64 {
	return avgRate / float64(stat.success)
}

func tsigKeyParse(s string) (algo, name, secret string, ok bool) {
	s1 := strings.SplitN(s, ":", 3)
	switch len(s1) {
	case 2:
		return "hmac-md5.sig-alg.reg.int.", dns.Fqdn(s1[0]), s1[1], true
	case 3:
		switch s1[0] {
		case "hmac-md5":
			return "hmac-md5.sig-alg.reg.int.", dns.Fqdn(s1[1]), s1[2], true
		case "hmac-sha1":
			return "hmac-sha1.", dns.Fqdn(s1[1]), s1[2], true
		case "hmac-sha256":
			return "hmac-sha256.", dns.Fqdn(s1[1]), s1[2], true
		}
	}
	return
}

func sectionCheck(set []dns.RR, server string, tcp bool) {
	var key *dns.DNSKEY
	for _, rr := range set {
		if rr.Header().Rrtype == dns.TypeRRSIG {
			var expired string
			if !rr.(*dns.RRSIG).ValidityPeriod(time.Now().UTC()) {
				expired = "(*EXPIRED*)"
			}
			rrset := getRRset(set, rr.Header().Name, rr.(*dns.RRSIG).TypeCovered)
			if dnskey == nil {
				key = getKey(rr.(*dns.RRSIG).SignerName, rr.(*dns.RRSIG).KeyTag, server, tcp)
			} else {
				key = dnskey
			}
			if key == nil {
				fmt.Printf(";? DNSKEY %s/%d not found\n", rr.(*dns.RRSIG).SignerName, rr.(*dns.RRSIG).KeyTag)
				continue
			}
			where := "net"
			if dnskey != nil {
				where = "disk"
			}
			if err := rr.(*dns.RRSIG).Verify(key, rrset); err != nil {
				fmt.Printf(";- Bogus signature, %s does not validate (DNSKEY %s/%d/%s) [%s] %s\n",
					shortSig(rr.(*dns.RRSIG)), key.Header().Name, key.KeyTag(), where, err.Error(), expired)
			} else {
				fmt.Printf(";+ Secure signature, %s validates (DNSKEY %s/%d/%s) %s\n", shortSig(rr.(*dns.RRSIG)), key.Header().Name, key.KeyTag(), where, expired)
			}
		}
	}
}

// Check the sigs in the msg, get the signer's key (additional query), get the
// rrset from the message, check the signature(s)
func sigCheck(in *dns.Msg, server string, tcp bool) {
	sectionCheck(in.Answer, server, tcp)
	sectionCheck(in.Ns, server, tcp)
	sectionCheck(in.Extra, server, tcp)
}

// Check if there is need for authenticated denial of existence check
func denialCheck(in *dns.Msg) {
	var denial []dns.RR
	// nsec(3) lives in the auth section
	for _, rr := range in.Ns {
		if rr.Header().Rrtype == dns.TypeNSEC {
			return
		}
		if rr.Header().Rrtype == dns.TypeNSEC3 {
			denial = append(denial, rr)
			continue
		}
	}

	if len(denial) > 0 {
		denial3(denial, in)
	}
	fmt.Printf(";+ Unimplemented: check for denial-of-existence for nsec\n")
	return
}

// NSEC3 Helper
func denial3(nsec3 []dns.RR, in *dns.Msg) {
	qname := in.Question[0].Name
	qtype := in.Question[0].Qtype
	switch in.Rcode {
	case dns.RcodeSuccess:
		// qname should match nsec3, type should not be in bitmap
		match := nsec3[0].(*dns.NSEC3).Match(qname)
		if !match {
			fmt.Printf(";- Denial, owner name does not match qname\n")
			fmt.Printf(";- Denial, failed authenticated denial of existence proof for no data\n")
			return
		}
		for _, t := range nsec3[0].(*dns.NSEC3).TypeBitMap {
			if t == qtype {
				fmt.Printf(";- Denial, found type, %d, in bitmap\n", qtype)
				fmt.Printf(";- Denial, failed authenticated denial of existence proof for no data\n")
				return
			}
			if t > qtype { // ordered list, bail out, because not found
				break
			}
		}
		// Some success data printed here
		fmt.Printf(";+ Denial, matching record, %s, (%s) found and type %s denied\n", qname,
			strings.ToLower(dns.HashName(qname, nsec3[0].(*dns.NSEC3).Hash, nsec3[0].(*dns.NSEC3).Iterations, nsec3[0].(*dns.NSEC3).Salt)),
			dns.TypeToString[qtype])
		fmt.Printf(";+ Denial, secure authenticated denial of existence proof for no data\n")
		return
	case dns.RcodeNameError: // NXDOMAIN Proof
		indx := dns.Split(qname)
		var ce string // Closest Encloser
		var nc string // Next Closer
		var wc string // Source of Synthesis (wildcard)
	ClosestEncloser:
		for i := 0; i < len(indx); i++ {
			for j := 0; j < len(nsec3); j++ {
				if nsec3[j].(*dns.NSEC3).Match(qname[indx[i]:]) {
					ce = qname[indx[i]:]
					wc = "*." + ce
					if i == 0 {
						nc = qname
					} else {
						nc = qname[indx[i-1]:]
					}
					break ClosestEncloser
				}
			}
		}
		if ce == "" {
			fmt.Printf(";- Denial, closest encloser not found\n")
			return
		}
		fmt.Printf(";+ Denial, closest encloser, %s (%s)\n", ce,
			strings.ToLower(dns.HashName(ce, nsec3[0].(*dns.NSEC3).Hash, nsec3[0].(*dns.NSEC3).Iterations, nsec3[0].(*dns.NSEC3).Salt)))
		covered := 0 // Both nc and wc must be covered
		for i := 0; i < len(nsec3); i++ {
			if nsec3[i].(*dns.NSEC3).Cover(nc) {
				fmt.Printf(";+ Denial, next closer %s (%s), covered by %s -> %s\n", nc, nsec3[i].Header().Name, nsec3[i].(*dns.NSEC3).NextDomain,
					strings.ToLower(dns.HashName(ce, nsec3[0].(*dns.NSEC3).Hash, nsec3[0].(*dns.NSEC3).Iterations, nsec3[0].(*dns.NSEC3).Salt)))
				covered++
			}
			if nsec3[i].(*dns.NSEC3).Cover(wc) {
				fmt.Printf(";+ Denial, source of synthesis %s (%s), covered by %s -> %s\n", wc, nsec3[i].Header().Name, nsec3[i].(*dns.NSEC3).NextDomain,
					strings.ToLower(dns.HashName(ce, nsec3[0].(*dns.NSEC3).Hash, nsec3[0].(*dns.NSEC3).Iterations, nsec3[0].(*dns.NSEC3).Salt)))
				covered++
			}
		}
		if covered != 2 {
			fmt.Printf(";- Denial, too many, %d, covering records\n", covered)
			fmt.Printf(";- Denial, failed authenticated denial of existence proof for name error\n")
			return
		}
		fmt.Printf(";+ Denial, secure authenticated denial of existence proof for name error\n")
		return
	}
}

// Return the RRset belonging to the signature with name and type t
func getRRset(l []dns.RR, name string, t uint16) []dns.RR {
	var l1 []dns.RR
	for _, rr := range l {
		if strings.ToLower(rr.Header().Name) == strings.ToLower(name) && rr.Header().Rrtype == t {
			l1 = append(l1, rr)
		}
	}
	return l1
}

// Get the key from the DNS (uses the local resolver) and return them.
// If nothing is found we return nil
func getKey(name string, keytag uint16, server string, tcp bool) *dns.DNSKEY {
	c := new(dns.Client)
	if tcp {
		c.Net = "tcp"
	}
	m := new(dns.Msg)
	m.SetQuestion(name, dns.TypeDNSKEY)
	m.SetEdns0(4096, true)
	r, _, err := c.Exchange(m, server)
	if err != nil {
		return nil
	}
	for _, k := range r.Answer {
		if k1, ok := k.(*dns.DNSKEY); ok {
			if k1.KeyTag() == keytag {
				return k1
			}
		}
	}
	return nil
}

// shortSig shortens RRSIG to "miek.nl RRSIG(NS)"
func shortSig(sig *dns.RRSIG) string {
	return sig.Header().Name + " RRSIG(" + dns.TypeToString[sig.TypeCovered] + ")"
}

// shortenMsg walks trough message and shortens Key data and Sig data.
func shortenMsg(in *dns.Msg) {
	for i, answer := range in.Answer {
		in.Answer[i] = shortRR(answer)
	}
	for i, ns := range in.Ns {
		in.Ns[i] = shortRR(ns)
	}
	for i, extra := range in.Extra {
		in.Extra[i] = shortRR(extra)
	}
}

func shortRR(r dns.RR) dns.RR {
	switch t := r.(type) {
	case *dns.DS:
		t.Digest = "..."
	case *dns.DNSKEY:
		t.PublicKey = "..."
	case *dns.RRSIG:
		t.Signature = "..."
	case *dns.NSEC3:
		t.Salt = "." // Nobody cares
		if len(t.TypeBitMap) > 5 {
			t.TypeBitMap = t.TypeBitMap[1:5]
		}
	}
	return r
}
