package main

import (
	"fmt"
	"net"
	"os"
)

var _ = net.Listen
var _ = os.Exit

func handleConnections(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 1024)
	for {

		_, err := conn.Read(buf)

		if err != nil {
			fmt.Println("Error reading commands: ", err.Error())
			os.Exit(1)
		}
		conn.Write([]byte("+OK\r\n"))
	}
}

func main() {
	fmt.Println("Logs from your program will appear here!")

	l, err := net.Listen("tcp", "0.0.0.0:6379")
	if err != nil {
		fmt.Println("Failed to bind to port 6379")
		os.Exit(1)
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connections: ", err.Error())
			os.Exit(1)
		}

		go handleConnections(conn)
	}
}
