package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

func Send(req Request) (*Response, error) {
	conn, err := net.DialTimeout("unix", SocketPath(), 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("focus daemon is not running (run `focus install` or `focus daemon`)")
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return nil, fmt.Errorf("read daemon response: %w", err)
	}
	return &response, nil
}
