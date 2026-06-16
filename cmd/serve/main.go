// Command serve hosts the contents of the web directory over HTTP so the
// WebAssembly page can be opened in a browser. The browser refuses to load a
// .wasm module directly from the file system, so a server is required.
//
// Usage:
//
//	serve [-addr :8080] [-dir web]
package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	dir := flag.String("dir", "web", "directory to serve")
	flag.Parse()

	log.Printf("serving %s at http://localhost%s", *dir, *addr)
	if err := http.ListenAndServe(*addr, http.FileServer(http.Dir(*dir))); err != nil {
		log.Fatal(err)
	}
}
