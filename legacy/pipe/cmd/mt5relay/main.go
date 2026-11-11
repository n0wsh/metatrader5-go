//go:build windows

// mt5relay exposes a MetaTrader 5 terminal's API pipe over TCP so that
// non-Windows hosts can use metatrader5.DialTCP. Run it on the same Windows
// machine (or Wine prefix) as terminal64.exe.
//
// The relay forwards raw bytes and has no authentication of its own: anyone
// who can reach the port controls the terminal. Bind it to localhost or a
// private network only.
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net"

	mt5 "github.com/n0wsh/metatrader5-go/legacy/pipe"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:5555", "TCP address to listen on")
	terminal := flag.String("terminal", `C:\Program Files\MetaTrader 5\terminal64.exe`, "path to terminal64.exe")
	pipe := flag.String("pipe", "", "pipe name (overrides -terminal)")
	flag.Parse()

	name := *pipe
	if name == "" {
		var err error
		if name, err = mt5.TerminalPipeName(*terminal); err != nil {
			log.Fatalf("resolve pipe for %s: %v", *terminal, err)
		}
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("relaying %s -> %s", ln.Addr(), name)
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go relay(c, name)
	}
}

func relay(c net.Conn, pipe string) {
	defer c.Close()
	p, err := mt5.DialPipeConn(context.Background(), pipe)
	if err != nil {
		log.Printf("%s: dial pipe: %v", c.RemoteAddr(), err)
		return
	}
	defer p.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(p, c); done <- struct{}{} }()
	go func() { io.Copy(c, p); done <- struct{}{} }()
	<-done
}
