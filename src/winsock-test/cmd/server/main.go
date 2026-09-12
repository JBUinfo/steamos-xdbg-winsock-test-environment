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
	messageDefault := os.Getenv("WS2_FIXTURE_MESSAGE")
	if messageDefault == "" {
		messageDefault = "ping from winsock server\n"
	}
	intervalDefault := 3 * time.Minute
	if value := os.Getenv("WS2_FIXTURE_INTERVAL"); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			intervalDefault = parsed
		}
	}
	port := flag.Int("port", portDefault, "TCP port to listen on localhost")
	interval := flag.Duration("interval", intervalDefault, "time between server messages")
	message := flag.String("message", messageDefault, "message sent each interval")
	once := flag.Bool("once", false, "send one message and exit")
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

	listener, err := winsock.Socket()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer winsock.Close(listener)

	address := winsock.Address(127, 0, 0, 1, uint16(*port))
	if err := winsock.Bind(listener, &address); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := winsock.Listen(listener, 8); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("winsock server listening on 127.0.0.1:%d (pid %d)\n", *port, os.Getpid())
	connection, err := winsock.Accept(listener)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer winsock.Close(connection)
	fmt.Printf("client connected; sending every %s\n", interval.String())
	for sequence := 1; ; sequence++ {
		time.Sleep(*interval)
		payload := fmt.Sprintf("%s", *message)
		if _, sendErr := winsock.Send(connection, []byte(payload)); sendErr != nil {
			fmt.Fprintln(os.Stderr, sendErr)
			os.Exit(1)
		}
		fmt.Printf("sent message %d: %q\n", sequence, payload)
		buffer := make([]byte, 256)
		count, receiveErr := winsock.Recv(connection, buffer)
		if receiveErr != nil {
			fmt.Fprintln(os.Stderr, receiveErr)
			os.Exit(1)
		}
		if count == 0 {
			fmt.Println("client closed the connection")
			return
		}
		fmt.Printf("received reply %d (%d bytes): %q\n", sequence, count, buffer[:count])
		if *once {
			return
		}
	}
}
