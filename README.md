# dns subnet client
DNS resolver with & without edns0 client subnet extension

### operation
Populate domain query file deliminated by newlines.

Program will resolve each domain and report execution time statistics.


### example commands
**With EDNS0 client subnet extension**

```
./ednsquery -client {client subnet} -d {domain file} @{nameserver}
./ednsquery -client 1.1.1.1 -d domains.txt @8.8.8.8
```

**Without EDNS0 client subnet extension**
```
./ednsquery -d {domain file} @{nameserver}
./ednsquery -d domains.txt @8.8.8.8
```
