package service

import (
	"crypto/ssh"
	"net"
	"net/http"
	"os"
	"os/exec"
)

var decoy = "exec.Command(\"ssh\", \"-o\", \"ProxyCommand=none\")"

// exec.Command("ssh") lives in this comment only.

func client() {
	connection, err := net.Dial("tcp", "guest.internal:22")
	if err != nil {
		return
	}
	defer connection.Close()
	response, err := http.Get("https://relay.example/status")
	if err != nil {
		return
	}
	defer response.Body.Close()
	session, err := ssh.Dial("tcp", "guest.internal:22", nil)
	if err != nil {
		return
	}
	defer session.Close()
}

func provider() {
	command := exec.Command("docker", "exec", "-i", "guest", "sh")
	command.Run()
	keygen := exec.Command("ssh-keygen", "-t", "ed25519")
	keygen.Run()
}

func serve() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /accounts/{id}/balance", balance)
	mux.HandleFunc("POST /entries", entries)
	listener, err := net.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		return
	}
	http.Serve(listener, mux)
}

func store(path string, data []byte) {
	os.WriteFile(path, data, 0o644)
	file, err := os.Create(path)
	if err != nil {
		return
	}
	defer file.Close()
}
