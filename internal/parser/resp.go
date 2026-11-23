package parser

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
)

type Value struct {
	Type  string
	Str   string
	Num   int
	Bulk  []byte
	Array []Value
}

func RespParser(reader *bufio.Reader) ([]byte, error) {

	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}

	switch prefix {
	case '*':
		s, _ := reader.ReadString('\r')
		fmt.Printf("%T", s)
	case '$':
		// Parse bulk string
	case '+':
		s, _ := reader.ReadString('\r')
		fmt.Println("%T", s)
	case ':':
		// Parse integer
	case '-':
		// Parse error
	default:
		return []byte{}, errors.New("invalid resp bytes prefix")
	}

	return []byte{}, nil
}

func handleCommand(value Value) (Value, error) {
	command := string(value.Array[0].Bulk)

	switch strings.ToUpper(command) {
	case "SET":
		// Extract key and value
		// Store in database
		// Return OK

	case "GET":
		// Extract key
		// Retrieve from database
		// Return bulk string

	case "DEL":
		// Delete key
		// Return integer

	default:
		// Return unknown command error
	}

	return Value{}, nil
}
