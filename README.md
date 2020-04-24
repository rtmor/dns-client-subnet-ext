# dns subnet client

DNS resolver with & without edns0 client subnet extension for statistic gathering.

### operation

Populate domain query file deliminated by newlines.

Program will resolve each domain and report execution time statistics.

### options

```
  -client string
        set edns client-subnet option
  -d string
        dns query wordlists file
  -ns string
        set preferred nameserver (default "8.8.8.8")
  -t int
        number of threads (default 100)
  -v    enable verbose output of dns queries
```

### example commands

**With EDNS0 client subnet extension**

```
./main -client {client subnet} -d {domain file} -ns {nameserver}
./main -client 1.1.1.1 -d domains.txt -ns 8.8.8.8
```

**Without EDNS0 client subnet extension**

```
./ednsquery -d {domain file} -ns {nameserver}
./ednsquery -d domains.txt -ns 8.8.8.8
```
