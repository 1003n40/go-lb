package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/google/uuid"
)

func simple_handler(hostname string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookies := r.Cookies()
		for _, cookie := range cookies {
			http.SetCookie(w, cookie)
			log.Printf("Cookie: %s, %s", cookie.Name, cookie.Value)
		}
		fmt.Fprintf(w, "Hello from: %s", hostname)
	}
}

func main() {
	hostname := uuid.New().String()
	http.HandleFunc("/", simple_handler(hostname))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	log.Printf("Starting service with name: %s...", hostname)
	addr := fmt.Sprintf("%s:%s", host, port)
	http.ListenAndServe(addr, nil)
}
