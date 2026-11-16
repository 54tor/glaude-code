package main

// Profilage opt-in : GLD_PPROF_ADDR=127.0.0.1:6060 ouvre les points de net/http/pprof sur une adresse de boucle locale, hors
// du serveur public et de son authentification. Sans la variable, rien n'est ouvert.

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time"
)

const pprofHeaderTimeout = 10 * time.Second

func startPprof() {
	address := os.Getenv("GLD_PPROF_ADDR")
	if address == "" {
		return
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		journal("GLD_PPROF_ADDR refusée : adresse de boucle locale exigée (127.0.0.1:port)")
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: pprofHeaderTimeout}
	go func() { journal("pprof sur http://%s/debug/pprof/ : %v", address, server.ListenAndServe()) }()
}
