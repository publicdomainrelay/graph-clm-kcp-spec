package main

import (
	"flag"
	"log"
	"net/http"

	"example.com/todo/httpapi"
	"example.com/todo/todo"
)

func main() {
	address := flag.String("addr", "127.0.0.1:8080", "address to listen on")
	flag.Parse()
	log.Printf("todo listening on %s", *address)
	if err := http.ListenAndServe(*address, httpapi.NewHandler(todo.NewStore())); err != nil {
		log.Fatal(err)
	}
}
