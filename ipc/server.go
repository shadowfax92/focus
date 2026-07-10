package ipc

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
)

type Handler func(Request) Response

func ListenAndServe(handler Handler) error {
	listener, err := Listen()
	if err != nil {
		return err
	}
	return Serve(listener, handler)
}

func Listen() (net.Listener, error) {
	path := SocketPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

func Serve(listener net.Listener, handler Handler) error {
	defer listener.Close()
	defer os.Remove(SocketPath())
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go handleConn(conn, handler)
	}
}

func removeStaleSocket(path string) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	conn, dialErr := net.DialTimeout("unix", path, 100_000_000)
	if dialErr == nil {
		conn.Close()
		return errors.New("focus daemon is already running")
	}
	return os.Remove(path)
}

func handleConn(conn net.Conn, handler Handler) {
	defer conn.Close()
	var request Request
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: "invalid request"})
		return
	}
	_ = json.NewEncoder(conn).Encode(handler(request))
}
