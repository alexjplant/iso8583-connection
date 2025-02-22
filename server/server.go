package server

import (
	"fmt"
	"net"
	"sync"

	"github.com/moov-io/iso8583"
	connection "github.com/moov-io/iso8583-connection"
)

// Server is a simple iso8583 server implementation currently used to test
// iso8583-client and most probably to be used for iso8583-test-harness
type Server struct {
	connectionOpts []connection.Option
	ln             net.Listener
	Addr           string
	wg             sync.WaitGroup

	closeCh chan bool

	// spec that will be used to unpack received messages
	spec *iso8583.MessageSpec

	// readMessageLength is the function that reads message length header
	// from the connection, decodes and returns message length
	readMessageLength connection.MessageLengthReader

	// writeMessageLength is the function that encodes message length and
	// writes message length header into the connection
	writeMessageLength connection.MessageLengthWriter
}

func New(spec *iso8583.MessageSpec, mlReader connection.MessageLengthReader, mlWriter connection.MessageLengthWriter, connectionOpts ...connection.Option) *Server {
	// automatically choose port
	return &Server{
		connectionOpts:     connectionOpts,
		closeCh:            make(chan bool),
		spec:               spec,
		readMessageLength:  mlReader,
		writeMessageLength: mlWriter,
	}
}

func (s *Server) Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.Addr = ln.Addr().String()
	s.ln = ln

	s.wg.Add(1)
	go func() {
		defer func() {
			s.wg.Done()
		}()

		for {
			conn, err := ln.Accept()
			if err != nil {
				// did we close the server?
				select {
				case <-s.closeCh:
					return
				default:
					// TODO: better handle errors
					fmt.Printf("Error accepting connection: %s\n", err.Error())
					return
				}
			}

			s.wg.Add(1)
			go func() {
				err := s.handleConnection(conn)
				if err != nil {
					fmt.Printf("Error handling connection: %s\n", err.Error())
				}
				s.wg.Done()
			}()
		}
	}()

	return nil
}

func (s *Server) Close() {
	close(s.closeCh)
	s.ln.Close()
	s.wg.Wait()
}

func (s *Server) handleConnection(conn net.Conn) error {
	c, err := connection.NewFrom(conn, s.spec, s.readMessageLength, s.writeMessageLength, s.connectionOpts...)
	if err != nil {
		return fmt.Errorf("creating connection: %w", err)
	}

	select {
	case <-s.closeCh:
		// if server was closed, close the client
		c.Close()
	case <-c.Done():
		// if client was closed (because of error or some internal action)
		// we just return
	}

	return nil
}
