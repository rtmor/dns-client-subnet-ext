# dns client subnet ext

DNS client tool for evaluating edns0 client subnet extension for statistic gathering.

### operation

Populate domain query file deliminated by newlines.

Program will resolve each domain and report execution time statistics with graph.

### usage

```
Usage: ./dns-client-subnet-ext [options] -ns {nameserver}

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
./dns-client-subnet-ext -client {client subnet} -d {domain file} -ns {nameserver}
./dns-client-subnet-ext 1.1.1.1 -d domains.txt -ns 8.8.8.8
```

**Without EDNS0 client subnet extension**

```
./dns-client-subnet-ext -d {domain file} -ns {nameserver}
./dns-client-subnet-ext -d domains.txt -ns 8.8.8.8
```
