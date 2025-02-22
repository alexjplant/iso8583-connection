package connection_test

import (
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/moov-io/iso8583"
	connection "github.com/moov-io/iso8583-connection"
	"github.com/moov-io/iso8583-connection/server"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/network"
	"github.com/moov-io/iso8583/prefix"
)

// here are the implementation of the provider protocol:
// * header reader and writer
// * spec
func readMessageLength(r io.Reader) (int, error) {
	header := network.NewBinary2BytesHeader()
	n, err := header.ReadFrom(r)
	if err != nil {
		return n, err
	}

	return header.Length(), nil
}

func writeMessageLength(w io.Writer, length int) (int, error) {
	header := network.NewBinary2BytesHeader()
	header.SetLength(length)

	n, err := header.WriteTo(w)
	if err != nil {
		return n, fmt.Errorf("writing message header: %w", err)
	}

	return n, nil
}

var testSpec *iso8583.MessageSpec = &iso8583.MessageSpec{
	Name: "ISO 8583 v1987 ASCII",
	Fields: map[int]field.Field{
		0: field.NewString(&field.Spec{
			Length:      4,
			Description: "Message Type Indicator",
			Enc:         encoding.ASCII,
			Pref:        prefix.ASCII.Fixed,
		}),
		1: field.NewBitmap(&field.Spec{
			Length:      8,
			Description: "Bitmap",
			Enc:         encoding.Binary,
			Pref:        prefix.Binary.Fixed,
		}),
		2: field.NewString(&field.Spec{
			Length:      19,
			Description: "Primary Account Number",
			Enc:         encoding.ASCII,
			Pref:        prefix.ASCII.LL,
		}),
		7: field.NewString(&field.Spec{
			Length:      10,
			Description: "Transmission Date & Time",
			Enc:         encoding.ASCII,
			Pref:        prefix.ASCII.Fixed,
		}),
		11: field.NewString(&field.Spec{
			Length:      6,
			Description: "Systems Trace Audit Number (STAN)",
			Enc:         encoding.ASCII,
			Pref:        prefix.ASCII.Fixed,
		}),
	},
}

// create testServer for testing
type testServer struct {
	Addr string

	server *server.Server

	// to protect following
	mutex         sync.Mutex
	receivedPings int
}

func (t *testServer) Ping() {
	t.mutex.Lock()
	t.receivedPings++
	t.mutex.Unlock()
}

func (t *testServer) ReceivedPings() int {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	return t.receivedPings
}

const (
	CardForDelayedResponse string = "4200000000000000"
	CardForPingCounter     string = "4005550000000019"
	// for sending incoming message with same STAN as
	// received message
	CardForSameSTANRequest string = "4012888888881881"
)

func NewTestServer() (*testServer, error) {
	var srv *testServer

	// define logic for our test server
	testServerLogic := func(c *connection.Connection, message *iso8583.Message) {
		mti, err := message.GetMTI()
		if err != nil {
			log.Printf("getting MTI: %s", err.Error())
			return
		}

		// we handle only 0800 messages
		if mti != "0800" {
			return
		}

		// update MTI for the response message
		newMTI := "0810"
		message.MTI(newMTI)

		// check if PAN was set to specific test case value
		f2 := message.GetField(2)
		if f2 != nil {
			code, err := f2.String()
			if err != nil {
				log.Printf("getting field 2: %s", err.Error())
				return
			}

			switch code {
			case CardForDelayedResponse:
				// testing value to "sleep" for a 3 seconds
				time.Sleep(500 * time.Millisecond)

			case CardForSameSTANRequest:
				// here we will send message to the client with
				// the same STAN
				stan, _ := message.GetString(11)
				incomingMessage := iso8583.NewMessage(testSpec)
				incomingMessage.MTI("0800")
				incomingMessage.Field(11, stan)

				_, err := c.Send(incomingMessage)
				if err != nil {
					log.Printf("sending message to client: %s", err.Error())
				}

				// and then delay the reply
				time.Sleep(200 * time.Millisecond)

			case CardForPingCounter:
				// ping request received
				srv.Ping()
			}
		}

		c.Reply(message)
	}

	server := server.New(testSpec, readMessageLength, writeMessageLength, connection.InboundMessageHandler(testServerLogic))
	// start on random port
	err := server.Start("127.0.0.1:")
	if err != nil {
		return nil, err
	}

	srv = &testServer{
		server: server,
		Addr:   server.Addr,
	}

	return srv, nil
}

func (t *testServer) Close() {
	t.server.Close()
}
