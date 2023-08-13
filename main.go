package main

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	hubPkg "github.com/mmcomp/go-aref_shop_chat_server/hub"
	redisPkg "github.com/mmcomp/go-aref_shop_chat_server/redis"
)

func init() {

	err := godotenv.Load(".env")

	if err != nil {
		log.Fatal("Error loading .env file")
	}
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL)
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, "home.html")
}

func main() {
	addr := os.Getenv("ADDR")
	redisAddr := os.Getenv("REDIS_ADDR")
	laravelChannel := os.Getenv("LARAVEL_CHANEL")
	nodeChannel := os.Getenv("NODE_CHANEL")
	userHash := os.Getenv("USERS_HASH")
	messageHash := os.Getenv("MESSAGES_HASH")
	messageCount, _ := strconv.Atoi(os.Getenv("MESSAGE_COUNT"))
	laravelPresenceChannel := os.Getenv("LARAVEL_PRESENCE_CHANEL")
	ssl := os.Getenv("SSL")
	test := os.Getenv("TEST")
	var ctx = context.Background()
	redisService := redisPkg.NewRedisService(ctx, redisAddr, nodeChannel, userHash, messageHash, messageCount, "", 0)
	hub := hubPkg.NewHub(redisService, laravelChannel, laravelPresenceChannel)
	go hub.Run()
	if ssl == "no" {
		if test == "yes" {
			http.HandleFunc("/", serveHome)
			http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
				hubPkg.ServeWs(hub, w, r)
			})
		} else {
			http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				hubPkg.ServeWs(hub, w, r)
			})
		}

		err := http.ListenAndServe(addr, nil)
		if err != nil {
			log.Fatal("ListenAndServe: ", err)
		}
	}

	serverTLSCert, err := tls.LoadX509KeyPair("./ssl/certificate.pem", "./ssl/privatekey.pem")
	if err != nil {
		log.Fatalf("Error loading certificate and key file: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverTLSCert},
	}
	server := http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hubPkg.ServeWs(hub, w, r)
		}),
		TLSConfig: tlsConfig,
	}
	defer server.Close()
	log.Fatal(server.ListenAndServeTLS("", ""))
}
