package main

// HSTS seulement pour l'adresse publique déclarée : l'en-tête couvre tout le nom d'hôte, quel que soit le port, et casserait un
// autre service HTTP servi sous le même nom ; une IP ou localhost n'en reçoit jamais.

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
)

const hstsHeaderValue = "max-age=31536000"

func hstsHost() string {
	publicURL, err := url.Parse(strings.TrimSpace(os.Getenv("GLD_PUBLIC_URL")))
	if err != nil || publicURL.Scheme != "https" {
		return ""
	}
	hostName := strings.ToLower(publicURL.Hostname())
	_, ipErr := netip.ParseAddr(hostName)
	isLocalName := hostName == "localhost" || strings.HasSuffix(hostName, ".localhost")
	if hostName == "" || ipErr == nil || isLocalName {
		return ""
	}
	return hostName
}

func setHSTS(responseWriter http.ResponseWriter, request *http.Request) {
	publicHost := hstsHost()
	if publicHost == "" || !requestHTTPS(request) {
		return
	}
	requestHost := request.Host
	if hostWithoutPort, _, splitErr := net.SplitHostPort(requestHost); splitErr == nil {
		requestHost = hostWithoutPort
	}
	if strings.EqualFold(requestHost, publicHost) {
		responseWriter.Header().Set("Strict-Transport-Security", hstsHeaderValue)
	}
}
