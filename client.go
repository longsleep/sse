package sse

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
)

var (
	// ErrNilChan will be returned by Notify if it is passed a nil channel
	ErrNilChan = fmt.Errorf("nil channel given")
)

// Client is the default client used for requests.
var DefaultClient = &http.Client{}

// Event is a go representation of an http server-sent event
type Event struct {
	URI string

	Type string
	ID   string
	Data io.Reader
}

type getReqFunc func(string, string, io.Reader) (*http.Request, error)

// DefaultGetReq is a function to return a single request. It will be used by notify to
// get a request and can be replaced if additional configuration is desired on
// the request. The "Accept" header will necessarily be overwritten.
var DefaultGetReq = func(method, uri string, body io.Reader) (*http.Request, error) {
	return http.NewRequest(method, uri, body)
}

func makeReqest(getReq getReqFunc, method, uri string, body io.Reader) (*http.Request, error) {
	if getReq == nil {
		getReq = DefaultGetReq
	}

	req, err := getReq(method, uri, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", sseContentType)

	return req, nil
}

// Notify takes the uri of an SSE stream, http.Client, request generator function and
// a channel, and will send an Event  down the channel when recieved, until the
// stream is closed. This is blocking, and so you will likely want to call this
//  in a new goroutine (via `go Notify(..)`). If client or getReq ire nil, the
// DefaultClient and DefaultGetReq values are used.
func Notify(uri string, client *http.Client, getReq getReqFunc, evCh chan<- *Event) error {
	if evCh == nil {
		return ErrNilChan
	}

	req, err := makeReqest(getReq, http.MethodGet, uri, nil)
	if err != nil {
		return fmt.Errorf("error getting sse request: %v", err)
	}

	if client == nil {
		client = DefaultClient
	}

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error performing sse request for %s: %v", uri, err)
	}

	scanner := bufio.NewScanner(res.Body)
	defer res.Body.Close()

	// Process event stream as defined in https://html.spec.whatwg.org/multipage/server-sent-events.html#event-stream-interpretation
	colon := []byte{':'}
	var currEvent *Event
	var bs []byte
	var field []byte
	var value []byte
	var buf *bytes.Buffer
	for scanner.Scan() {
		bs = scanner.Bytes()
		if len(bs) == 0 {
			// Empty line, dispatch.
			if currEvent != nil {
				evCh <- currEvent
				currEvent = nil
			}
			buf = nil
			continue
		}
		if bs[0] == colon[0] {
			// Ignore comments.
			continue
		}
		spl := bytes.SplitN(bs, colon, 2)
		if len(spl) == 2 {
			// Have colon.
			field = spl[0]
			// Use rest as value, trim leading space if there.
			value = bytes.TrimLeft(spl[1], " ")
		} else {
			// No colon, treat full value as field with empty value.
			field = bs
			value = []byte("")
		}
		if currEvent == nil {
			currEvent = &Event{URI: uri}
		}
		// Process field.
		switch f := string(field); f {
		case keyEvent:
			currEvent.Type = string(value)
		case keyData:
			if buf == nil {
				buf = bytes.NewBuffer(value)
				currEvent.Data = buf
			} else {
				buf.Write(value)
				buf.WriteByte('\n')
			}
		case keyID:
			currEvent.ID = string(value)
		}

	}
	return scanner.Err()
}
