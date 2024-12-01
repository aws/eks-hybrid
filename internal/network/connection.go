package network

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"
)

const dialTimeout = 5 * time.Second

// CheckConnectionToHost checks if a connection can be established to the host
// specified in the URL.
func CheckConnectionToHost(ctx context.Context, url url.URL) error {
	port := url.Port()
	if port == "" && url.Scheme == "https" {
		port = "443"
	}

	host := fmt.Sprintf("%s:%s", url.Hostname(), port)

	conn, err := net.DialTimeout("tcp", host, dialTimeout)
	if err != nil {
		return fmt.Errorf("dialing %s: %w", host, err)
	}
	defer conn.Close()

	return nil
}
