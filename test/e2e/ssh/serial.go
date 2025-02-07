package ssh

import (
	"errors"
	"fmt"
	"io"
	"math"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

// SerialConsole allows to get the serial console of a machine via SSH.
type SerialConsole struct {
	network, addr string
	config        *ClientConfig

	client  *ssh.Client
	session *ssh.Session
}

type ClientConfig = ssh.ClientConfig // define alias to avoid package name conflict in consumers

func NewSerialConsole(network, addr string, config *ClientConfig) *SerialConsole {
	return &SerialConsole{
		network: network,
		addr:    addr,
		config:  config,
	}
}

// Copy starts a new SSH session and copies the serial output stdout and stderr to dst.
// It starts go routines so it won't block. The caller should call Close when done.
func (o *SerialConsole) Copy(dst io.Writer) error {
	var err error
	o.client, err = dialSSH(o.network, o.addr, o.config)
	if err != nil {
		return fmt.Errorf("connecting to serial console: %w", err)
	}

	o.session, err = o.client.NewSession()
	if err != nil {
		return fmt.Errorf("creating ssh session: %w", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // enable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	if err := o.session.RequestPty("xterm", 80, 40, modes); err != nil {
		return fmt.Errorf("requesting pty: %w", err)
	}

	stdout, err := o.session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("opening stdout: %w", err)
	}

	stderr, err := o.session.StderrPipe()
	if err != nil {
		return fmt.Errorf("opening stderr: %w", err)
	}

	stdin, err := o.session.StdinPipe()
	if err != nil {
		return fmt.Errorf("opening stdin: %w", err)
	}

	err = o.session.Shell()
	if err != nil {
		return fmt.Errorf("opening shell: %w", err)
	}

	// sending a newline to "start" the output collection
	// since we aren't running a new command just connecting
	// to the serial console to capture output from the boot/init processes
	_, err = stdin.Write([]byte("\n"))
	if err != nil {
		return fmt.Errorf("writing to stdin: %w", err)
	}

	go func() {
		if _, err := io.Copy(dst, stdout); err != nil {
			fmt.Printf("Error copying serial console stdout: %v\n", err)
		}
	}()
	go func() {
		if _, err := io.Copy(dst, stderr); err != nil {
			fmt.Printf("Error copying serial console stderr: %v\n", err)
		}
	}()

	return nil
}

func (o *SerialConsole) Close() error {
	if o.session != nil {
		if err := o.session.Close(); err != nil {
			return fmt.Errorf("closing ssh session: %w", err)
		}
	}
	if o.client != nil {
		if err := o.client.Close(); err != nil {
			return fmt.Errorf("closing ssh client: %w", err)
		}
	}

	return nil
}

const (
	maxRetries  = 5
	backoffTime = 1 * time.Second
)

func dialSSH(network, addr string, config *ClientConfig) (*ssh.Client, error) {
	var err error
	for retry := range maxRetries {
		var client *ssh.Client
		client, err = ssh.Dial(network, addr, config)
		if err == nil {
			return client, nil
		}

		// We only retry on connection reset errors
		if !errors.Is(err, syscall.ECONNRESET) {
			return nil, err
		}

		// Exponential backoff
		time.Sleep(backoffTime * time.Duration(math.Floor(math.Pow(2, float64(retry)))))
	}

	return nil, fmt.Errorf("dialing SSH to serial console reached max amount of retries: %w", err)
}
