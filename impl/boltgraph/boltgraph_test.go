package boltgraph

import (
	"fmt"
	"io"
	"testing"
)

func TestATransientClassifierCoversALostConnection(t *testing.T) {
	broken := []error{
		fmt.Errorf("boltgraph: run: ConnectivityError: write tcp 127.0.0.1:1->127.0.0.1:2: write: broken pipe"),
		fmt.Errorf("boltgraph: collect: ConnectivityError: read tcp: connection reset by peer"),
		fmt.Errorf("boltgraph: run: dial tcp 127.0.0.1:7687: connect: connection refused"),
		io.ErrUnexpectedEOF,
	}
	for _, err := range broken {
		if !transient(err) {
			t.Errorf("transient(%v) = false, want a retry", err)
		}
	}
	if transient(fmt.Errorf("boltgraph: run: Neo.ClientError.Statement.SyntaxError: invalid query")) {
		t.Error("a query error must not be retried")
	}
	if transient(nil) {
		t.Error("nil is not transient")
	}
}
