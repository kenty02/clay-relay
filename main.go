package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/net/websocket"
	"io"
	"log"
	"os"
	"strconv"
	"unsafe"
)

var (
	webSocketClientMessage   = make(chan string)
	webSocketClientConnected = false
	nativeHostMessage        = make(chan string)
)

const disableConnectionCheck = false
const allowRemoteViewer = false

func handleWebSocket(c echo.Context) error {
	// 一旦wsを受け付けた後closeだとtrpcのwsLinkでreconnectの無限ループが発生することがあったためここで弾く
	if webSocketClientConnected && !disableConnectionCheck {
		return echo.NewHTTPError(400, "Client already connected")
	}
	websocket.Server{Handler: websocket.Handler(func(ws *websocket.Conn) {
		Trace.Print("WebSocket client connected")
		defer func(ws *websocket.Conn) {
			err := ws.Close()
			if err != nil {
				Error.Printf("Unable to close websocket connection: %v", err)
			}
		}(ws)

		sendRelayMessage("open")

		webSocketClientConnected = true
		defer func() { webSocketClientConnected = false }()

		receiveErr := make(chan error)
		go func() {
			for {
				m := ""
				err := websocket.Message.Receive(ws, &m)
				if err != nil {
					receiveErr <- err
					return
				}
				webSocketClientMessage <- m
			}
		}()
	ClientLoop:
		for {
			select {
			case msg := <-nativeHostMessage:
				err := websocket.Message.Send(ws, msg)
				if err != nil {
					c.Logger().Error(err)
					break ClientLoop
				}
			case receiveErr := <-receiveErr:
				c.Logger().Error(receiveErr)
				break ClientLoop
			}
		}
		sendRelayMessage("close")
	})}.ServeHTTP(c.Response(), c.Request())
	return nil
}

func main() {
	// if arg is "--register", call registerNativeMessagingHost and exit
	if len(os.Args) > 1 && os.Args[1] == "--register" {
		err := registerNativeMessagingHost()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		fmt.Println("Registered")
		return
	}

	file, err := os.OpenFile("chrome-native-host-log.txt" /*os.O_APPEND|*/, os.O_CREATE|os.O_WRONLY, 0644)
	//err = errors.New("debug mode")
	if err != nil {
		Init(os.Stderr, os.Stderr)
		Error.Printf("Unable to create and/or open log file. Will log to Stderr. Error: %v", err)
	} else {
		Init(file, file)
		// ensure we close the log file when we're done
		defer file.Close()
	}

	e := echo.New()
	e.Use(middleware.Logger())
	e.Static("/", "public")
	e.GET("/ws", handleWebSocket)
	e.Logger.SetOutput(Trace.Writer())
	e.HideBanner = true
	e.HidePort = true
	const port = 3003
	Trace.Print("Listening on port " + strconv.Itoa(port))
	serverErr := make(chan error)
	defer close(serverErr)
	go func() {
		var address string
		if allowRemoteViewer {
			address = ""
		} else {
			address = "127.0.0.1"
		}
		serverErr <- e.Start(address + ":" + strconv.Itoa(port)) // todo doesn't throw error on port in use
	}()

	Trace.Printf("Clay relay started with byte order: %v", nativeEndian)
	disconnected := make(chan struct{})
	defer close(disconnected)
	go func() {
		read()
		disconnected <- struct{}{}
	}()

	sendRelayMessage("This is clay-relay")
	go func() {
		for {
			select {
			case msg := <-webSocketClientMessage:
				send(msg) // todo error handling
			}
		}
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			Error.Printf("WebSocket server error: %v", err)
		} else {
			Trace.Print("WebSocket server stopped without error.")
		}
	case <-disconnected:
		Trace.Print("Disconnected.")
	}
}

type RelayMessage struct {
	RelayMessage string `json:"relayMessage"`
}

func sendRelayMessage(msg string) {
	relayMessage := RelayMessage{msg}
	relayMessageJson, err := json.Marshal(relayMessage)
	if err != nil {
		Error.Printf("Unable to marshal relay message: %v", err)
		return
	}
	sendBytes(relayMessageJson)
}

/*
* @Author: J. Farley
* @Date: 2019-05-19
* @Description: Basic chrome native messaging host example.
 */

// constants for Logger
var (
	// Trace logs general information messages.
	Trace *log.Logger
	// Error logs error messages.
	Error *log.Logger
)

// nativeEndian used to detect native byte order
var nativeEndian binary.ByteOrder

// bufferSize used to set size of IO buffer - adjust to accommodate message payloads
var bufferSize = 8192

// Init initializes logger and determines native byte order.
func Init(traceHandle io.Writer, errorHandle io.Writer) {
	Trace = log.New(traceHandle, "TRACE: ", log.Ldate|log.Ltime|log.Lshortfile)
	Error = log.New(errorHandle, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)

	// determine native byte order so that we can read message size correctly
	var one int16 = 1
	b := (*byte)(unsafe.Pointer(&one))
	if *b == 0 {
		nativeEndian = binary.BigEndian
	} else {
		nativeEndian = binary.LittleEndian
	}
}

// read Creates a new buffered I/O reader and reads messages from Stdin.
func read() {
	v := bufio.NewReader(os.Stdin)
	// adjust buffer size to accommodate your json payload size limits; default is 4096
	s := bufio.NewReaderSize(v, bufferSize) // todo create buffer size based on message size
	Trace.Printf("IO buffer reader created with buffer size of %v.", s.Size())

	lengthBytes := make([]byte, 4)
	lengthNum := int(0)

	// we're going to indefinitely read the first 4 bytes in buffer, which gives us the message length.
	// if stdIn is closed we'll exit the loop and shut down host
	for b, err := s.Read(lengthBytes); b > 0 && err == nil; b, err = s.Read(lengthBytes) {
		// convert message length bytes to integer value
		lengthNum = readMessageLength(lengthBytes)
		Trace.Printf("Message size in bytes: %v", lengthNum)

		// If message length exceeds size of buffer, the message will be truncated.
		// This will likely cause an error when we attempt to unmarshal message to JSON.
		if lengthNum > bufferSize {
			Error.Printf("Message size of %d exceeds buffer size of %d. Message will be truncated and is unlikely to unmarshal to JSON.", lengthNum, bufferSize)
		}

		// read the content of the message from buffer
		content := make([]byte, lengthNum)
		_, err := s.Read(content)
		if err != nil && err != io.EOF {
			Error.Fatal(err)
		}

		// message has been read, now parse and process
		parseMessage(content)
	}

	Trace.Print("Stdin closed.")
}

// readMessageLength reads and returns the message length value in native byte order.
func readMessageLength(msg []byte) int {
	var length uint32
	buf := bytes.NewBuffer(msg)
	err := binary.Read(buf, nativeEndian, &length)
	if err != nil {
		Error.Printf("Unable to read bytes representing message length: %v", err)
	}
	return int(length)
}

// parseMessage parses incoming message
func parseMessage(msg []byte) {
	iMsg := string(msg)
	Trace.Printf("Message received: %s", msg)
	nativeHostMessage <- iMsg
}

func send(msg string) {
	byteMsg := []byte(msg)
	sendBytes(byteMsg)
}

func sendBytes(msg []byte) {
	writeMessageLength(msg)

	var msgBuf bytes.Buffer
	_, err := msgBuf.Write(msg)
	if err != nil {
		Error.Printf("Unable to write message length to message buffer: %v", err)
	}

	_, err = msgBuf.WriteTo(os.Stdout)
	if err != nil {
		Error.Printf("Unable to write message buffer to Stdout: %v", err)
	}
}

// writeMessageLength determines length of message and writes it to os.Stdout.
func writeMessageLength(msg []byte) {
	err := binary.Write(os.Stdout, nativeEndian, uint32(len(msg)))
	if err != nil {
		Error.Printf("Unable to write message length to Stdout: %v", err)
	}
}
