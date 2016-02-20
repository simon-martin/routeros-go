// Go support for communicating with MikroTic RouerOS
// Implementation from docs and Python example at http://wiki.mikrotik.com/wiki/Manual:API

package routeros_api

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/golang/glog"
	"io"
	"net"
	"regexp"
)

// A RouterOS client
type Client struct {
	Host     string
	Port     int
	User     string
	Password string
	conn     net.Conn
}

// RouterOS talks in Senetences, that are a command and zero or more attributes
type Sentence struct {
	Command    string
	Attributes map[string]string
}

// Open a connection to RouterOS
func (client *Client) Connect() error {
	var err error

	glog.V(1).Infoln("Connecting...")
	conn_string := fmt.Sprintf("%s:%d", client.Host, client.Port)
	client.conn, err = net.Dial("tcp", conn_string)
	if err != nil {
		return err
	}

	// First login command get's a seed for our hash
	glog.V(2).Infoln("Getting seed...")
	s := Sentence{Command: "/login"}
	reply, err := client.RunCommand(s)
	if err != nil {
		return err
	}

	// Hash seed with our password
	seed, err := hex.DecodeString(reply[0].Attributes["=ret"])
	hash := md5.New()
	io.WriteString(hash, "\000")
	io.WriteString(hash, client.Password)
	hash.Write(seed)

	// Build the attributes for our login sentence
	attributes := make(map[string]string)
	attributes["=name"] = client.User
	attributes["=response"] = fmt.Sprintf("00%x", hash.Sum(nil))
	s.Attributes = attributes

	// Run login command again with attributes
	glog.V(2).Infoln("Logging in...")
	reply, err = client.RunCommand(s)
	if err != nil {
		return err
	}

	// TODO Maybe there are other reasons other?
	if reply[0].Attributes["=ret"] == "error" {
		err = errors.New("Access denied")
		client.conn.Close()
		client.conn = nil
	}

	return err
}

// Close the current session
func (client *Client) Close() {
	client.conn.Close()
}

// Send a sentence to RouterOS and return any reply
func (client *Client) RunCommand(command Sentence) ([]Sentence, error) {
	var reply []Sentence
	var err error

	glog.V(1).Infoln("Ruuning command:", command.Command)
	write_sentence(client.conn, command)

	for {
		sentence := read_sentence(client.conn)
		if len(sentence.Command) == 0 {
			continue
		}

		if sentence.Command == "!trap" {
			err = errors.New(sentence.Attributes["=message"])
		}

		reply = append(reply, sentence)

		if sentence.Command == "!done" {
			break
		}
	}

	return reply, err
}

// Break a sentence into words and send them
func write_sentence(conn net.Conn, sentence Sentence) {
	glog.V(2).Infoln("Sending sentence:", sentence)
	write_word(conn, sentence.Command)
	for attribute, value := range sentence.Attributes {
		write_word(conn, fmt.Sprintf("%s=%s", attribute, value))
	}
	write_word(conn, "")
}

// Send an encoded length and the word
func write_word(conn net.Conn, word string) {
	write_length(conn, int64(len(word)))
	glog.V(3).Infoln("Sending word:", word)
	conn.Write([]byte(word))
}

// This is copied from http://wiki.mikrotik.com/wiki/Manual:API#Example_client
// I'll confess I don't really understand it
// It seems to be massively over-engineered to save sending a few bytes
func write_length(conn net.Conn, length int64) {
	var ba []byte
	switch {
	case length < 0x80:
		ba = make([]byte, 1)
		ba[0] = byte(length)
	case length < 0x4000:
		length |= 0x8000
		ba = make([]byte, 2)
		ba[0] = byte(length>>8) & 0xFF
		ba[1] = byte(length) & 0xFF
	case length < 0x200000:
		length |= 0xC00000
		ba = make([]byte, 3)
		ba[0] = byte(length>>16) & 0xFF
		ba[1] = byte(length>>8) & 0xFF
		ba[2] = byte(length) & 0xFF
	case length < 0x10000000:
		length |= 0xE0000000
		ba = make([]byte, 4)
		ba[0] = byte(length>>24) & 0xFF
		ba[1] = byte(length>>16) & 0xFF
		ba[2] = byte(length>>8) & 0xFF
		ba[3] = byte(length) & 0xFF
	default:
		ba = make([]byte, 5)
		ba[0] = 0xF0
		ba[1] = byte(length>>24) & 0xFF
		ba[2] = byte(length>>16) & 0xFF
		ba[3] = byte(length>>8) & 0xFF
		ba[4] = byte(length) & 0xFF
	}
	glog.V(4).Infoln("Sending", ba, "bytes")
	conn.Write(ba)
}

// Read words from a connection until there are no more and return a sentence
// struct
func read_sentence(conn net.Conn) Sentence {
	glog.V(2).Infoln("Reading sentence...")
	var words []string
	for {
		word := read_word(conn)
		if len(word) == 0 {
			sentence := parse_sentence(words)
			glog.V(2).Infoln("Read sentence:", sentence)
			return sentence
		}
		words = append(words, word)
	}
	glog.Fatal("This should never be seen but 1.02 compiler compains")
	return Sentence{}
}

// Build a sentence struct from a slice of words
func parse_sentence(words []string) Sentence {
	glog.V(2).Infoln("Parsing sentence:", words)
	if len(words) == 0 {
		return Sentence{}
	}

	word_regex, _ := regexp.Compile("^(=.*)(?:=(.*))$")
	sentence := Sentence{Command: words[0]}
	attributes := make(map[string]string)
	for _, word := range words[1:] {
		matches := word_regex.FindStringSubmatch(word)
		attributes[matches[1]] = matches[2]
	}
	sentence.Attributes = attributes
	return sentence
}

// Read a word by getting it's length then reading that many bytes
func read_word(conn net.Conn) string {
	length := read_length(conn)
	glog.V(3).Infoln("Reading", length, "bytes")
	data := make([]byte, length)
	conn.Read(data)
	return string(data)
}

// This is copied from http://wiki.mikrotik.com/wiki/Manual:API#Example_client
// I'll confess I don't really understand it
// It seems to be massively over-engineered to save sending a few bytes
func read_length(conn net.Conn) int64 {
	glog.V(4).Infoln("Reading length...")
	length := read_byte(conn)
	switch {
	case (length & 0x80) == 0x00:
	case (length & 0xC0) == 0x80:
		length &= ^0xC0
		length <<= 8
		length += read_byte(conn)
	case (length & 0xE0) == 0xC0:
		length &= ^0xE0
		length <<= 8
		length += read_byte(conn)
		length <<= 8
		length += read_byte(conn)
	case (length & 0xF0) == 0xE0:
		length &= ^0xF0
		length <<= 8
		length += read_byte(conn)
		length <<= 8
		length += read_byte(conn)
		length <<= 8
		length += read_byte(conn)
	case (length & 0xF8) == 0xF0:
		length = read_byte(conn)
		length <<= 8
		length += read_byte(conn)
		length <<= 8
		length += read_byte(conn)
		length <<= 8
		length += read_byte(conn)
	}
	return length
}

// Need to pull 1 byte at a time to decode the length of the word
func read_byte(conn net.Conn) int64 {
	b := make([]byte, 1)
	conn.Read(b)
	return int64(b[0])
}
