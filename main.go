package main

import (
	"encoding/json"
	"flag"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	"github.com/thinxer/semikami"
	"golang.org/x/net/context"
)

var (
	flagAPI = flag.String("api", ":8080", "API Binding Address")
)

func bigEndian(hi, lo byte) int {
	return (int(hi) << 8) + int(lo)
}

func CORS(ctx context.Context, w http.ResponseWriter, r *http.Request) context.Context {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return nil
	}
	return ctx
}

func WriteJSON(w http.ResponseWriter, v interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}

type M map[string]interface{}

var (
	currentPM25  int32
	currentPM100 int32
)

func WSHandler(c *websocket.Conn) {
	closed := false
	ret := websocket.CloseError{
		Code: websocket.CloseNormalClosure,
	}
	const writeWait = 10 * time.Second

	defer func() {
		log.Infof("Close websocket [%s]", ret.Error())
		c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(ret.Code, ret.Text), time.Now().Add(writeWait))
		c.Close()
	}()

	go func() {
		for {
			_, r, err := c.NextReader()
			if err != nil {
				break
			}
			io.Copy(ioutil.Discard, r)
		}
		log.Infof("Pipe closed")

		closed = true
	}()

	for !closed {
		signal := M{
			"pm2.5": atomic.LoadInt32(&currentPM25),
			"pm10":  atomic.LoadInt32(&currentPM100),
		}
		if err := c.WriteJSON(signal); err != nil && err != websocket.ErrCloseSent {
			if _, ok := err.(net.Error); !ok {
				log.Warnf("Error writing to client: %s", err.Error())
			}
			closed = true
		}
		time.Sleep(2 * time.Second)
	}
}

func main() {
	flag.Parse()

	go func() {
		for {
			err := SerialWaitSignal()
			if err != nil {
				log.Error(err)
				continue
			}
			data, err := SerialReadFrame()
			if err != nil {
				log.Error(err)
				continue
			}
			pm10 := bigEndian(data[0], data[1])
			pm25 := bigEndian(data[2], data[3])
			pm100 := bigEndian(data[4], data[5])
			log.Infof("PM1.0: %d, PM2.5: %d, PM10: %d", pm10, pm25, pm100)

			atomic.StoreInt32(&currentPM25, int32(pm25))
			atomic.StoreInt32(&currentPM100, int32(pm100))
		}
	}()

	router := httprouter.New()
	k := kami.New(nil, router).With(CORS)

	k.Get("/realtime", func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
		upg := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// accept any origin
				return true
			},
		}
		ws, err := upg.Upgrade(w, r, nil)
		if err != nil {
			log.Warnf("Error to upgrade: %s", err.Error())
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		go WSHandler(ws)
	})

	k.Get("/", func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/viewer.html", http.StatusMovedPermanently)
	})

	http.Handle("/", k.Handler())

	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	log.Infof("Listen at %s", *flagAPI)
	panic(http.ListenAndServe(*flagAPI, http.DefaultServeMux))
}
