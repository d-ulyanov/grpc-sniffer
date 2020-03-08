package httpstream

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/d-ulyanov/grpc-sniffer/hello"
	"github.com/d-ulyanov/grpc-sniffer/pb2json"
	"github.com/golang/protobuf/descriptor"
	"golang.org/x/net/http2"
	"io"
	"log"

	"github.com/golang/protobuf/proto"
)

var pathMsgs map[string][]proto.Message

func dumpMsg(net string, path string, frame *http2.DataFrame, side int) {
	buf := frame.Data()
	id := frame.Header().StreamID
	compress := buf[0]
	// length := binary.BigEndian.Uint32(buf[1:5])
	if compress == 1 {
		// use compression, check Message-Encoding later
		log.Printf("%s %d use compression, msg %q", net, id, buf[5:])
		return
	}

	// Em, a little ugly here, try refactor later.
	if msgs, ok := pathMsgs[path]; ok {
		switch side {
		case 1:
			msg := proto.Clone(msgs[0])
			if err := proto.Unmarshal(buf[5:], msg); err == nil {
				log.Printf("%s %d %s %s", net, id, path, msg)
				return
			}
		case 2:
			msg := proto.Clone(msgs[1])
			if err := proto.Unmarshal(buf[5:], msg); err == nil {
				log.Printf("%s %d %s %s", net, id, path, msg)
				return
			}
		default:
			// We can't know the data is request or response
			for _, msg := range msgs {
				msg := proto.Clone(msg)
				if err := proto.Unmarshal(buf[5:], msg); err == nil {
					log.Printf("%s %d %s %s", net, id, path, msg)
					return
				}
			}
		}
	}

	helloReq := &hello.HelloRequest{}
	reqDesc, _ := descriptor.ForMessage(helloReq)

	jsonReq, err := pb2json.Unmarshal(buf[5:], reqDesc, "HelloRequest")
	if err != nil {
		fmt.Println(err.Error())
	}

	fmt.Println("GRPC Sniffer: ", string(jsonReq))


	//dumpProto(net, id, path, buf[5:])
}

func dumpProto(net string, id uint32, path string, buf []byte) {
	var out bytes.Buffer
	if err := decodeProto(&out, buf, 0); err != nil {
		// decode failed
		log.Printf("%s %d %s %q", net, id, path, buf)
	} else {
		log.Printf("%s %d %s\n%s", net, id, path, out.String())
	}
}

func decodeProto(out *bytes.Buffer, buf []byte, depth int) error {
out:
	for {
		if len(buf) == 0 {
			return nil
		}

		for i := 0; i < depth; i++ {
			out.WriteString("  ")
		}

		op, n := proto.DecodeVarint(buf)
		if n == 0 {
			return io.ErrUnexpectedEOF
		}

		buf = buf[n:]

		tag := op >> 3
		wire := op & 7

		switch wire {
		default:
			fmt.Fprintf(out, "tag=%d unknown wire=%d\n", tag, wire)
			break out
		case proto.WireBytes:
			l, n := proto.DecodeVarint(buf)
			if n == 0 {
				return io.ErrUnexpectedEOF
			}
			buf = buf[n:]
			if len(buf) < int(l) {
				return io.ErrUnexpectedEOF
			}

			// Here we can't know the raw bytes is string, or embedded message
			// So we try to parse like a embedded message at first
			outLen := out.Len()
			fmt.Fprintf(out, "tag=%d struct\n", tag)
			if err := decodeProto(out, buf[0:int(l)], depth+1); err != nil {
				// Seem this is not a embedded message, print raw buffer
				out.Truncate(outLen)
				fmt.Fprintf(out, "tag=%d bytes=%q\n", tag, buf[0:int(l)])
			}
			buf = buf[l:]
		case proto.WireFixed32:
			if len(buf) < 4 {
				return io.ErrUnexpectedEOF
			}
			u := binary.LittleEndian.Uint32(buf[0:4])
			buf = buf[4:]
			fmt.Fprintf(out, "tag=%d fix32=%d\n", tag, u)
		case proto.WireFixed64:
			if len(buf) < 8 {
				return io.ErrUnexpectedEOF
			}
			u := binary.LittleEndian.Uint64(buf[0:8])
			buf = buf[4:]
			fmt.Fprintf(out, "tag=%d fix64=%d\n", tag, u)
		case proto.WireVarint:
			u, n := proto.DecodeVarint(buf)
			if n == 0 {
				return io.ErrUnexpectedEOF
			}
			buf = buf[n:]
			fmt.Fprintf(out, "tag=%d varint=%d\n", tag, u)
		case proto.WireStartGroup:
			fmt.Fprintf(out, "tag=%d start\n", tag)
			depth++
		case proto.WireEndGroup:
			fmt.Fprintf(out, "tag=%d end\n", tag)
			depth--
		}
	}
	return io.ErrUnexpectedEOF
}
