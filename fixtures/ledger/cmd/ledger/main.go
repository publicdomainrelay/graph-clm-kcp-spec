package main

import (
	"flag"
	"log"
	"net/http"

	"example.com/ledger/domain"
	"example.com/ledger/httpapi"
)

func main() {
	address := flag.String("addr", "127.0.0.1:8080", "address to listen on")
	flag.Parse()
	log.Printf("ledger listening on %s", *address)
	if err := http.ListenAndServe(*address, httpapi.NewHandler(domain.NewLedger())); err != nil {
		log.Fatal(err)
	}
}
