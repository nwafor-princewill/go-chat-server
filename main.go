package main

import (
	// "encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Message represents a chat message
type Message struct {
	Username string `json:"username"`
	Text     string `json:"text"`
	Time     string `json:"time"`
}

// Client represents a connected user
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan Message
	username string
}

// Hub manages all connected clients and message broadcasting
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan Message
	register   chan *Client
	unregister chan *Client
}

// Configure WebSocket upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all connections in development
	},
}

// Create new hub
func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run hub forever - this is the brain of our chat server
func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			// New client connected
			h.clients[client] = true
			log.Printf("Client registered: %s. Total clients: %d", client.username, len(h.clients))

		case client := <-h.unregister:
			// Client disconnected
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				log.Printf("Client unregistered: %s. Total clients: %d", client.username, len(h.clients))
			}

		case message := <-h.broadcast:
			// Broadcast message to all clients
			for client := range h.clients {
				select {
				case client.send <- message:
					// Message sent successfully
				default:
					// Couldn't send - client might be stuck
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// Handle reading messages FROM client
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		var msg Message
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			break
		}

		// Add timestamp and username
		msg.Time = time.Now().Format("15:04:05")
		if msg.Username == "" {
			msg.Username = c.username
		}

		log.Printf("Message from %s: %s", msg.Username, msg.Text)
		
		// Send to hub for broadcasting
		c.hub.broadcast <- msg
	}
}

// Handle writing messages TO client  
func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()

	for {
		message, ok := <-c.send
		if !ok {
			// Channel closed
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}

		err := c.conn.WriteJSON(message)
		if err != nil {
			break
		}
	}
}

// Handle new WebSocket connections
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	// Get username from query parameter or use default
	username := r.URL.Query().Get("username")
	if username == "" {
		username = "Anonymous"
	}

	// Create new client
	client := &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan Message, 256),
		username: username,
	}

	// Register client with hub
	client.hub.register <- client

	// Start client's read and write pumps
	go client.writePump()
	go client.readPump()
}

func main() {
	// Create and start hub
	hub := newHub()
	go hub.run()

	// Serve static files (our HTML frontend)
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	// WebSocket endpoint
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	// Start server
	port := ":8080"
	fmt.Printf("🚀 Go Chat Server starting on http://localhost%s\n", port)
	fmt.Printf("💬 Broadcast mode: All users see all messages!\n")
	
	log.Fatal(http.ListenAndServe(port, nil))
}