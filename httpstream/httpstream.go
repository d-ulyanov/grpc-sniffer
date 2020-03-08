package httpstream

import (
	"bufio"
	"fmt"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/google/gopacket"
	"github.com/google/gopacket/tcpassembly"
	"github.com/google/gopacket/tcpassembly/tcpreader"
)

var (
	streamPath = map[string]map[uint32]string{}
	pathLock   sync.RWMutex
)

// Build a simple HTTP request parser using tcpassembly.StreamFactory and tcpassembly.Stream interfaces

// httpStreamFactory implements tcpassembly.StreamFactory
type HTTPStreamFactory struct{}

func (h *HTTPStreamFactory) New(net, transport gopacket.Flow) tcpassembly.Stream {
	hstream := &HTTPStream{
		net:       net,
		transport: transport,
		r:         tcpreader.NewReaderStream(),
	}
	go hstream.run() // Important... we must guarantee that data from the reader stream is read.

	// ReaderStream implements tcpassembly.Stream, so we can return a pointer to it.
	return &hstream.r
}

// HTTPStream will handle the actual decoding of http requests.
type HTTPStream struct {
	net, transport gopacket.Flow
	r              tcpreader.ReaderStream
}

func (h *HTTPStream) run() {
	buf := bufio.NewReader(&h.r)
	framer := http2.NewFramer(ioutil.Discard, buf)
	framer.MaxHeaderListSize = uint32(16 << 20)
	framer.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	net := fmt.Sprintf("%s:%s -> %s:%s", h.net.Src(), h.transport.Src(), h.net.Dst(), h.transport.Dst())
	revNet := fmt.Sprintf("%s:%s -> %s:%s", h.net.Dst(), h.transport.Dst(), h.net.Src(), h.transport.Src())
	// 1 request, 2 response, 0 unkonwn
	var streamSide = map[uint32]int{}

	defer func() {
		pathLock.Lock()
		delete(streamPath, net)
		delete(streamPath, revNet)
		pathLock.Unlock()
	}()
	for {
		//fmt.Println("got something...")

		peekBuf, err := buf.Peek(9)
		if err == io.EOF {
			return
		} else if err != nil {
			log.Print("Error reading frame", h.net, h.transport, ":", err)
			continue
		}

		prefix := string(peekBuf)

		// log.Printf("%s prefix %q", net, prefix)
		if ok, err := ParseRequest(net, prefix, buf); ok || err != nil {
			continue
		}

		if ok, err := ParseResponse(net, prefix, buf); ok || err != nil {
			continue
		}

		if strings.HasPrefix(prefix, "PRI") {
			buf.Discard(len(connPreface))
		}

		frame, err := framer.ReadFrame()
		if err == io.EOF {
			return
		}

		if err != nil {
			log.Print("Error reading frame", h.net, h.transport, ":", err)
			continue
		}

		id := frame.Header().StreamID
		// log.Printf("%s id %d frame %v", net, id, frame.Header())
		switch frame := frame.(type) {
		case *http2.MetaHeadersFrame:
			//fmt.Println("got headers frame")


			for _, hf := range frame.Fields {
				// log.Printf("%s id %d %s=%s", net, id, hf.Name, hf.Value)
				if hf.Name == ":path" {
					// TODO: remove stale stream ID
					pathLock.Lock()
					_, ok := streamPath[net]
					if !ok {
						streamPath[net] = map[uint32]string{}
					}
					streamPath[net][id] = hf.Value
					pathLock.Unlock()
					streamSide[id] = 1
				} else if hf.Name == ":status" {
					streamSide[id] = 2
				}
			}
		case *http2.DataFrame:
			//fmt.Println("got data frame")

			var path string
			pathLock.RLock()
			nets, ok := streamPath[net]
			if !ok {
				nets, ok = streamPath[revNet]
			}

			if ok {
				path = nets[id]
			}

			pathLock.RUnlock()
			// log.Printf("%s id %d path %s, data", net, id, path)
			dumpMsg(net, path, frame, streamSide[id])
		default:
		}
	}
}

const connPreface string = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

func ParseRequest(net string, prefix string, buf *bufio.Reader) (bool, error) {
	prefix = strings.ToUpper(prefix)
	if strings.HasPrefix(prefix, "GET") || strings.HasPrefix(prefix, "POST") ||
		strings.HasPrefix(prefix, "PUT") || strings.HasPrefix(prefix, "DELET") ||
		strings.HasPrefix(prefix, "HEAD") {
		r, err := http.ReadRequest(buf)
		if err != nil {
			return false, err
		}

		log.Printf("%s Req %v", net, r)

		buf.Discard(int(r.ContentLength))
		r.Body.Close()
		return true, nil
	}
	return false, nil
}

func ParseResponse(net string, prefix string, buf *bufio.Reader) (bool, error) {
	prefix = strings.ToUpper(prefix)
	if strings.HasPrefix(prefix, "HTTP") {
		resp, err := http.ReadResponse(buf, nil)
		if err != nil {
			return false, err
		}

		log.Printf("%s Resp %v", net, resp)

		buf.Discard(int(resp.ContentLength))
		resp.Body.Close()
		return true, nil
	}
	return false, nil
}
