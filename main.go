package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/skerkour/rz"
	"github.com/skerkour/rz/log"
	"golang.org/x/net/websocket"
	"io"
	"net"
	"os"
	"strconv"
	"time"
	"unsafe"
)

var (
	webSocketClientMessage   = make(chan string)
	webSocketClientConnected = false
	initialMessageChan       = make(chan string)
	extensionTrpcMessageChan = make(chan string)
	initialMessageReceived   = false
)

const disableConnectionCheck = false
const allowRemoteViewer = false

var token string

func handleWebSocket(c echo.Context) error {
	if c.QueryParams().Get("token") != token {
		return echo.NewHTTPError(403, "Invalid token")
	}
	// 一旦wsを受け付けた後closeだとtrpcのwsLinkでreconnectの無限ループが発生することがあったためここで弾く
	if webSocketClientConnected && !disableConnectionCheck {
		return echo.NewHTTPError(400, "Client already connected")
	}
	websocket.Server{Handler: websocket.Handler(func(ws *websocket.Conn) {
		log.Info("WebSocket client connected")
		defer func(ws *websocket.Conn) {
			err := ws.Close()
			if err != nil {
				log.Error("Unable to close websocket connection", rz.Error("error", err))
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
			case msg := <-extensionTrpcMessageChan:
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
	if len(os.Args) > 1 {
		if os.Args[1] == "--register" {
			err := registerNativeMessagingHost()
			if err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				return
			}
			fmt.Println("Registered")
			return
		} else if os.Args[1] == "--unregister" {
			err := unregisterNativeMessagingHost()
			if err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				return
			}
			fmt.Println("Unregistered")
			return
		}
	}
	Init()

	file, err := os.OpenFile("clay-relay-log.txt" /*os.O_APPEND|*/, os.O_CREATE|os.O_WRONLY, 0644)
	//err = errors.New("debug mode")
	if err != nil {
		log.SetLogger(rz.New(rz.Writer(os.Stderr)))
		log.Error("Unable to open log file, using stderr instead", rz.Error("error", err))
	} else {
		writer := rz.SyncWriter(file)
		log.SetLogger(rz.New(rz.Writer(writer)))
		defer func(file *os.File) {
			_ = file.Close()
		}(file)
	}

	log.Info("Clay relay started", rz.Any("byte_order", nativeEndian))
	disconnected := make(chan struct{})
	defer close(disconnected)
	go func() {
		read()
		disconnected <- struct{}{}
	}()

	var firstExtensionMessage Message
	var initialMessagePayload InitialMessagePayload
	select {
	case initialMessageRaw := <-initialMessageChan:
		close(initialMessageChan)
		initialMessageReceived = true
		err = json.Unmarshal([]byte(initialMessageRaw), &firstExtensionMessage)
		if err != nil {
			log.Error("Unable to parse first message", rz.Error("error", err))
			println("Did you mean to run this program with --register or --unregister?")
			os.Exit(1)
			return
		}
		if firstExtensionMessage.Action != "init" {
			log.Error("First message was not initial message", rz.Any("message", firstExtensionMessage))
			println("Did you mean to run this program with --register or --unregister?")
			os.Exit(1)
			return
		}
		err = json.Unmarshal(firstExtensionMessage.Payload, &initialMessagePayload)
		if err != nil {
			log.Error("Unable to parse initial message payload", rz.Error("error", err))
			os.Exit(1)
			return
		}
	case <-time.After(500 * time.Millisecond):
		log.Error("No initial message received from native host")
		println("Did you mean to run this program with --register or --unregister?")
		os.Exit(1)
		return
	}

	// generate token
	b := make([]byte, 16)
	_, err = rand.Read(b)
	if err != nil {
		log.Error("Unable to generate token", rz.Error("error", err))
	}
	token = hex.EncodeToString(b)

	e := echo.New()
	e.Use(middleware.Logger())
	e.Static("/", "public")
	e.GET("/ws", handleWebSocket)
	e.Logger.SetOutput(io.Discard)
	e.HideBanner = true
	e.HidePort = true
	serverErr := make(chan error)
	defer close(serverErr)
	go func() {
		var address string
		if allowRemoteViewer {
			address = ""
		} else {
			address = "127.0.0.1"
		}
		serverErr <- e.Start(address + ":0")
	}()

	ms := 0
	for e.Listener == nil {
		time.Sleep(1 * time.Millisecond)
		ms += 1
	}
	log.Debug("Waited for listener", rz.Int("ms", ms), rz.String("address", e.Listener.Addr().String()))

	port := e.ListenerAddr().(*net.TCPAddr).Port
	log.Info("Listening started", rz.Int("port", port), rz.String("token", token))

	sendRelayMessage("This is clay-relay at port " + strconv.Itoa(port) + ", token " + token + ".")

	// don't create relay info if we're running in CI
	if os.Getenv("CI") == "" {
		relayInfo, err := newRelayInfo(port, initialMessagePayload.Tags)
		if err != nil {
			log.Error("Unable to create relay info", rz.Error("error", err))
			return
		}
		defer func() {
			err := relayInfo.Close()
			if err != nil {
				log.Error("Unable to close relay info", rz.Error("error", err))
			}
		}()
	}

	nmhError := make(chan error)
	go func() {
		for {
			select {
			case msg := <-webSocketClientMessage:
				// only 1MB messages are allowed to chrome
				if len(msg) > 1024*1024 {
					nmhError <- errors.New("message too large")
					// abort handling
					return
				}
				sendTrpc(msg) // todo error handling
			}
		}
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			log.Error("WebSocket server error", rz.Error("error", err))
		} else {
			log.Info("WebSocket server stopped without error.")
		}
	case <-disconnected:
		log.Info("Disconnected.")
	case err := <-nmhError:
		log.Error("Native messaging host error", rz.Error("error", err))
	}

	log.Info("Clay relay stopped.", rz.Int("largestMessageSize", largestMessageSize))
}

type Message struct {
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload"`
}

func sendRelayMessage(msg string) {
	payload := msg
	payloadJson, err := json.Marshal(payload)
	if err != nil {
		log.Error("Unable to marshal payload", rz.Error("error", err))
		return
	}
	nativeHostMessage := Message{"relayMessage", payloadJson}
	nativeHostMessageJson, err := json.Marshal(nativeHostMessage)
	if err != nil {
		log.Error("Unable to marshal native host message", rz.Error("error", err))
		return
	}
	sendBytes(nativeHostMessageJson)
}

// InitialMessagePayload from native host to relay
type InitialMessagePayload struct {
	Tags []string `json:"tags"`
}

/*
* @Author: J. Farley
* @Date: 2019-05-19
* @Description: Basic chrome native messaging host example.
 */

// nativeEndian used to detect native byte order
var nativeEndian binary.ByteOrder

// Init determines native byte order.
func Init() {
	// determine native byte order so that we can read message size correctly
	var one int16 = 1
	b := (*byte)(unsafe.Pointer(&one))
	if *b == 0 {
		nativeEndian = binary.BigEndian
	} else {
		nativeEndian = binary.LittleEndian
	}
}

var largestMessageSize int = 0

// read Creates a new buffered I/O reader and reads messages from Stdin.
func read() {
	lengthBytes := make([]byte, 4)
	lengthNum := int(0)

	// we're going to indefinitely read the first 4 bytes in buffer, which gives us the message length.
	// if stdIn is closed we'll exit the loop and shut down host
	for b, err := io.ReadFull(os.Stdin, lengthBytes); b > 0 && err == nil; b, err = io.ReadFull(os.Stdin, lengthBytes) {
		// convert message length bytes to integer value
		lengthNum = readMessageLength(lengthBytes)
		//log.Info("Message size in bytes: %v", lengthNum)
		if lengthNum > largestMessageSize {
			largestMessageSize = lengthNum
		}

		// read the content of the message from buffer
		content := make([]byte, lengthNum)
		/*		size, err := s.Read(content)
				log.Info("actual message size %v", size)
				if err != nil && err != io.EOF {
					Error.Fatal(err)
				}
		*/
		_, err := io.ReadFull(os.Stdin, content)
		if err != nil {
			log.Fatal("Failed to read native message content", rz.Error("error", err))
		}

		// message has been read, now parse and process
		parseMessage(content)
	}

	log.Info("Stdin closed.")
}

// readMessageLength reads and returns the message length value in native byte order.
func readMessageLength(msg []byte) int {
	var length uint32
	buf := bytes.NewBuffer(msg)
	err := binary.Read(buf, nativeEndian, &length)
	if err != nil {
		log.Error("Unable to read bytes representing message length", rz.Error("error", err))
	}
	return int(length)
}

// parseMessage parses incoming message
func parseMessage(msg []byte) {
	//log.Info("Message received: %s", msg)
	// if not closed
	if !initialMessageReceived {
		iMsg := string(msg)
		initialMessageChan <- iMsg
		return
	}
	message := Message{}
	err := json.Unmarshal(msg, &message)
	if err != nil {
		log.Error("Unable to parse message", rz.Error("error", err))
		return
	}
	switch message.Action {
	case "trpc":
		payloadString := ""
		err := json.Unmarshal(message.Payload, &payloadString)
		if err != nil {
			log.Error("Unable to parse payload", rz.Error("error", err))
			return
		}
		extensionTrpcMessageChan <- payloadString
	case "init":
		log.Error("Received init message more than once", rz.String("message", string(msg)))
	default:
		log.Error("Unknown message action %v")

	}

}

func sendTrpc(msg string) {
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Error("Unable to marshal payload", rz.Error("error", err))
		return
	}
	nativeHostMessage := Message{"trpc", payload}
	nativeHostMessageJson, err := json.Marshal(nativeHostMessage)
	if err != nil {
		log.Error("Unable to marshal native host message", rz.Error("error", err))
		return
	}
	sendBytes(nativeHostMessageJson)
}

func sendBytes(msg []byte) {
	writeMessageLength(msg)

	var msgBuf bytes.Buffer
	_, err := msgBuf.Write(msg)
	if err != nil {
		log.Error("Unable to write message length to message buffer", rz.Error("error", err))
	}

	_, err = msgBuf.WriteTo(os.Stdout)
	if err != nil {
		log.Error("Unable to write message buffer to Stdout", rz.Error("error", err))
	}
}

// writeMessageLength determines length of message and writes it to os.Stdout.
func writeMessageLength(msg []byte) {
	err := binary.Write(os.Stdout, nativeEndian, uint32(len(msg)))
	if err != nil {
		log.Error("Unable to write message length to Stdout", rz.Error("error", err))
	}
}
