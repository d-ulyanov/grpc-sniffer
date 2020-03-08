package main

import (
	"context"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	pb "github.com/d-ulyanov/grpc-sniffer/hello"
)

const (
	address     = "localhost:50051"
	defaultName = "world"
)

func main() {
	// Set up a connection to the server.
	conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

	// Contact the server and print out its response.
	name := defaultName
	if len(os.Args) > 1 {
		name = os.Args[1]
	}

	t := time.NewTicker(time.Second * 3)

	for range t.C {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		r, err := c.SayHello(ctx, &pb.HelloRequest{Name: name, MyField: "fucking_test"})
		if err != nil {
			log.Fatalf("could not greet: %v", err)
		}
		log.Printf("GRPC Client (sent): %s", r.GetMessage())
	}

	select {}
}
