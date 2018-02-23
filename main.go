package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"github.com/cryptix/wav"
	"github.com/gorilla/websocket"
	"gopkg.in/gin-gonic/gin.v1"
	log "gopkg.in/inconshreveable/log15.v2"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	HOST = "localhost"
	PORT = 8001
	CLI  = ""
)

type App struct {
	Clients map[*websocket.Conn]bool
	sync.RWMutex
}

var addr = flag.String("addr", "localhost:8080", "http service address")
var upgrader = websocket.Upgrader{}

func formatLog(r *log.Record) []byte {
	logString := fmt.Sprintf("[%s] %s", r.Lvl, r.Msg)

	return []byte(logString)
}
func echo(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Debug("upgrade:", err)
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Debug("read:", err)
			break
		}
		log.Debug("recv: %s", message)
		err = c.WriteMessage(mt, message)
		if err != nil {
			log.Debug("write:", err)
			break
		}
	}
}
func (app *App) RegisterClient(ws *websocket.Conn) {

	app.Lock()
	defer app.Unlock()

	app.Clients[ws] = true
}

func (app *App) UnregisterClient(ws *websocket.Conn) {

	app.Lock()
	defer app.Unlock()

	app.Clients[ws] = false
}

func (app *App) GetClients() []*websocket.Conn {

	app.RLock()
	defer app.RUnlock()

	list := make([]*websocket.Conn, len(app.Clients))

	var x int

	for c, _ := range app.Clients {

		list[x] = c

		x++
	}

	return list
}

type NCCO []struct {
	Action   string   `json:"action"`
	Text     string   `json:"text,omitempty"`
	EventURL []string `json:"eventUrl,omitempty"`
	From     string   `json:"from,omitempty"`
	Endpoint Endpoint `json:"endpoint,omitempty"`
}

type Endpoint []struct {
	Type        string  `json:"type"`
	URI         string  `json:"uri"`
	ContentType string  `json:"content-type"`
	Headers     Headers `json:"headers"`
}

type Headers struct {
	App string `json:"app"`
	Cli string `json:"cli"`
}

func (app *App) HandlerNCCO(c *gin.Context) {

	ncco := &NCCO{
		{
			Action: "talk",
			Text:   "Please wait while we connect you",
		},
		{
			Action: "connect",
			EventURL: []string{
				fmt.Sprintf("http://%s:%d/event", HOST, PORT),
			},
			From: CLI,
			Endpoint: Endpoint{
				{
					Type:        "websocket",
					URI:         fmt.Sprintf("http://%s:%d/socket", HOST, PORT+1),
					ContentType: "audio/l16;rate=16000",
					Headers: Headers{
						App: "soundboard",
						Cli: CLI,
					},
				},
			},
		},
	}
	c.JSON(200, ncco)
}
func (app *App) HandlerEvent(c *gin.Context) {

	c.String(200, "ok")
}

func (app *App) HandlerSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := wsupgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("Failed to set websocket upgrade: %+v", err)
		return
	}
	app.RegisterClient(conn)

	for conn == nil {
		time.Sleep(time.Second)
	}
	app.UnregisterClient(conn)

}
func (app *App) HandlerSound(w http.ResponseWriter, r *http.Request) {
	conn, err := wsupgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("Failed to set websocket upgrade: %+v", err)
		return
	}

	log.Debug("Client connected!")

	for {
		log.Debug("Waiting to receive!")

		time.Sleep(time.Second)

		t, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		msg := string(data)
		if len(msg) == 0 {
			continue
		}
		log.Debug("Streaming file: " + msg)

		testFile := fmt.Sprintf("audio/%s.wav", msg)

		testInfo, err := os.Stat(testFile)
		if err != nil {
			panic(err)
		}

		testWav, err := os.Open(testFile)
		if err != nil {
			panic(err)
		}

		wavReader, err := wav.NewReader(testWav, testInfo.Size())
		if err != nil {
			panic(err)
		}
		buf := new(bytes.Buffer)
		var numSamples = 0

		for {

			sample, err := wavReader.ReadRawSample()
			if err == io.EOF {
				log.Info("-------------------------------- EOF")
				break
			} else if err != nil {
				panic(err)
			}

			err = binary.Write(buf, binary.LittleEndian, sample)
			if numSamples == 160 {
				for _, c := range app.GetClients() {
					c.WriteMessage(t, buf.Bytes())
				}
				buf.Reset()
				numSamples = 0
			}
			numSamples = numSamples + 1
		}

	}

	log.Debug("Client disconnected!")
}

var wsupgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func main() {

	Log := log.New()
	sysH, _ := log.SyslogHandler(0, "websound", log.FormatFunc(
		func(r *log.Record) []byte {
			b := &bytes.Buffer{}
			m2 := make(map[string]string)
			for i := 0; i < len(r.Ctx); i += 2 {
				m2[(r.Ctx[i]).(string)] = fmt.Sprintf("%v", r.Ctx[i+1])
			}
			fmt.Fprintf(b, "[%s] %s %v", r.Lvl, m2["caller"], r.Msg)
			return b.Bytes()
		}),
	)
	logHandler := log.CallerFileHandler(sysH)
	Log.SetHandler(log.MultiHandler(
		log.StreamHandler(os.Stdout, log.TerminalFormat()),
		logHandler))

	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	app := &App{
		Clients: map[*websocket.Conn]bool{},
	}
	r.GET("/ncco", app.HandlerNCCO)
	r.GET("/event", app.HandlerEvent)
	r.POST("/event", app.HandlerEvent)
	r.StaticFile("/", "index.html")
	r.GET("/browser", func(c *gin.Context) {
		app.HandlerSound(c.Writer, c.Request)
	})
	r.GET("/socket", func(c *gin.Context) {
		app.HandlerSocket(c.Writer, c.Request)
	})

	/*
		http.HandleFunc("/echo", echo)
		http.Handle("/browser", websocket.Handler(app.HandlerSound))
		http.Handle("/socket", websocket.Handler(app.HandlerSocket
	*/
	r.Run(":8080")
}
