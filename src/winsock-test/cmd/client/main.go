package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"time"

	"ws2-hook-runner/winsock-test/internal/winsock"
)

func main() {
	portDefault := 27015
	if value := os.Getenv("WS2_FIXTURE_PORT"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			portDefault = parsed
		}
	}
	replyDefault := os.Getenv("WS2_FIXTURE_REPLY")
	if replyDefault == "" {
		replyDefault = "reply from winsock client\n"
	}
	port := flag.Int("port", portDefault, "TCP port on localhost")
	reply := flag.String("reply", replyDefault, "reply sent after each server message")
	once := flag.Bool("once", false, "receive one message and exit")
	flag.Parse()
	if *port < 1 || *port > 65535 {
		fmt.Fprintln(os.Stderr, "port must be between 1 and 65535")
		os.Exit(2)
	}

	if err := winsock.Startup(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer winsock.Cleanup()

	socket, err := winsock.Socket()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer winsock.Close(socket)

	address := winsock.Address(127, 0, 0, 1, uint16(*port))
	if err := winsock.Connect(socket, &address); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	buffer := make([]byte, 256)
	fmt.Printf("winsock client connected to 127.0.0.1:%d (pid %d)\n", *port, os.Getpid())
	for sequence := 1; ; sequence++ {
		count, err := winsock.Recv(socket, buffer)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if count == 0 {
			fmt.Println("server closed the connection")
			return
		}
		fmt.Printf("received message %d (%d bytes): %q\n", sequence, count, buffer[:count])
		response := fmt.Sprintf("%s", *reply)
		if _, err := winsock.Send(socket, []byte(response)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("sent reply %d: %q\n", sequence, response)
		if *once {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
