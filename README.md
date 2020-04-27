# dns subnet client

DNS resolver with & without edns0 client subnet extension for statistic gathering.

### operation

Populate domain query file deliminated by newlines.

Program will resolve each domain and report execution time statistics with graph.

### usage

```
Usage: ./dns-subnet-client [options] -ns {nameserver}

  -client string
        set edns client-subnet option
  -d string
        dns query wordlists file
  -ns string
        set preferred nameserver (default "8.8.8.8")
  -o string
        output directory for data graph (default "data")
  -t int
        number of threads (default 100)
  -v    enable verbose output of dns queries (debug)
```

### example commands

**With EDNS0 client subnet extension**

```
./dns-subnet-client -client {client subnet} -d {domain file} -ns {nameserver}
./dns-subnet-client -client 1.1.1.1 -d domains.txt -ns 8.8.8.8
```

**Without EDNS0 client subnet extension**

```
./dns-subnet-client -d {domain file} -ns {nameserver}
./dns-subnet-client -d domains.txt -ns 8.8.8.8
```
