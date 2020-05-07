# dns client subnet ext

DNS client tool for evaluating the authoritative name server application of edns0 Client Subnet Extension (CSE) for security and load-balancing purposes.

### operation

Domain lists deliminated by newlines are provided within the resources directory.

Program will attempt to resolve each domain at ~1,000 req/s and report execution time statistics with output graph.

### usage

```
Usage: ./dns-client-subnet-ext [options] -ns {nameserver}
  -c string
        Client subnet address
  -d string
        Location of domain list file
  -ns string
        DNS server address (ip) (default "8.8.8.8")
  -o string
        Location of output directory (default "output")
  -pps int
        Send up to PPS DNS queries per second (default 2000)
  -retries int
        Number of attempts made to resolve a domain (default 1)
  -rr string
        Resend unanswered query after RETRY (default "1s")
  -t int
        Number of concurrent workers (default 1000)
  -v    Verbose logging
```

### example commands

**With EDNS0 client subnet extension**

```
./dns-client-subnet-ext -c {client subnet} -d {domain file} -ns {nameserver}
./dns-client-subnet-ext -c 0.0.0.0 -d resources/majestic-domains.txt -ns 8.8.8.8
```

**Without EDNS0 client subnet extension**

```
./dns-client-subnet-ext -d {domain file} -ns {nameserver}
./dns-client-subnet-ext -d resources/majestic-domains.txt -ns 8.8.8.8
```
