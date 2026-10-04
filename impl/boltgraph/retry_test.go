package boltgraph

import (
	"errors"
	"fmt"
	"testing"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestTransientErrorsAreRetried(t *testing.T) {
	deadlock := &neo4j.Neo4jError{Code: "Neo.TransientError.Transaction.DeadlockDetected", Msg: "deadlock"}
	if !transient(fmt.Errorf("boltgraph: delete: %w", deadlock)) {
		t.Error("a wrapped deadlock is not reported as transient")
	}
	if !transient(&neo4j.Neo4jError{Code: "Neo.TransientError.General.DatabaseUnavailable"}) {
		t.Error("a transient general error is not reported as transient")
	}
	if transient(&neo4j.Neo4jError{Code: "Neo.ClientError.Statement.SyntaxError"}) {
		t.Error("a syntax error is reported as transient")
	}
	if transient(errors.New("connection refused")) {
		t.Error("a plain error is reported as transient")
	}
}
