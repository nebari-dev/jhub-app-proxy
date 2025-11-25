package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for testing
	},
}

func echoHandler(w http.ResponseWriter, r *http.Request) {
	// Check if this is a WebSocket upgrade request
	if websocket.IsWebSocketUpgrade(r) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Failed to upgrade connection: %v", err)
			return
		}
		defer conn.Close()

		log.Printf("WebSocket connection established from %s", r.RemoteAddr)

		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("Error reading message: %v", err)
				}
				break
			}

			log.Printf("Received message: %s", message)

			// Echo the message back
			err = conn.WriteMessage(messageType, message)
			if err != nil {
				log.Printf("Error writing message: %v", err)
				break
			}

			log.Printf("Echoed message: %s", message)
		}

		log.Printf("WebSocket connection closed")
	} else {
		// Regular HTTP request - respond with 200 OK for health checks
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("WebSocket echo server"))
		log.Printf("HTTP request from %s", r.RemoteAddr)
	}
}

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	flag.Parse()

	http.HandleFunc("/", echoHandler)

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("WebSocket echo server starting on %s", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
