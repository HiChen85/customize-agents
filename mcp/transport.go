package mcp

type Transport interface {
	Send(msg any) error
	Receive() ([]byte, error)
	Close() error
}
