package main

import (
	"fmt"
	"os"
	"strconv"

	"example.com/calc/calc"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: calc <left> <op> <right>")
		os.Exit(2)
	}
	left, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	right, err := strconv.Atoi(os.Args[3])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	switch os.Args[2] {
	case "+":
		fmt.Println(calc.Add(left, right))
	case "*":
		fmt.Println(calc.Multiply(left, right))
	default:
		fmt.Fprintf(os.Stderr, "unknown operator %q\n", os.Args[2])
		os.Exit(2)
	}
}
