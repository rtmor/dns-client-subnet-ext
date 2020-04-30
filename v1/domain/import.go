package domain

import (
	"bufio"
	"fmt"
	"log"
	"os"
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
