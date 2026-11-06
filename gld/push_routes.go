package main

import (
	"net/http"
	"net/url"
	"strings"
)

const (
	pushEndpointMaxLength = 2048
	pushBodyMaxBytes      = 8 << 10
)

func pushCallerSession(request *http.Request) string {
	gate := gateFrom(request)
	if gate == nil || gate.p == nil {
		return ""
	}
	return gate.p.SessionID
}

func pushCallerID(request *http.Request) int64 {
	gate := gateFrom(request)
	if gate == nil || gate.p == nil || gate.p.User == nil {
		return 0
	}
	return gate.p.User.ID
}

type pushSubscribeBody struct {
	Endpoint string
	Keys     struct{ P256dh, Auth string }
	Asking   bool
	Waiting  bool
	Lang     string
}

var pushServiceHostSuffixes = []string{
	"fcm.googleapis.com",
	"push.services.mozilla.com",
	"notify.windows.com",
	"web.push.apple.com",
}

// pushEndpointAllowed : le serveur postera sur cet endpoint ; seuls les services push des navigateurs sont acceptés (SSRF).
func pushEndpointAllowed(endpoint string) bool {
	if len(endpoint) > pushEndpointMaxLength {
		return false
	}
	parsed, parseErr := url.Parse(endpoint)
	if parseErr != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" {
		return false
	}
	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	for _, allowedSuffix := range pushServiceHostSuffixes {
		if hostname == allowedSuffix || strings.HasSuffix(hostname, "."+allowedSuffix) {
			return true
		}
	}
	return false
}

func (body pushSubscribeBody) isValid() bool {
	return pushEndpointAllowed(body.Endpoint) && body.Keys.P256dh != "" && body.Keys.Auth != ""
}

func routesPush(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/push/key", func(responseWriter http.ResponseWriter, request *http.Request) {
		publicKey, _, keysErr := pushVapidKeys()
		if keysErr != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", keysErr.Error())
			return
		}
		writeJSON(responseWriter, http.StatusOK, map[string]string{"key": publicKey})
	})
	mux.HandleFunc("PUT /v1/push/subscriptions", func(responseWriter http.ResponseWriter, request *http.Request) {
		var body pushSubscribeBody
		if !decodeBody(responseWriter, request, &body, pushBodyMaxBytes) {
			return
		}
		if !body.isValid() {
			writeErr(responseWriter, http.StatusBadRequest, "bad_request", L("srv.http_util.requeteInvalide"))
			return
		}
		upsertErr := pushSubscriptionUpsert(pushSubscription{
			Endpoint: body.Endpoint, P256dh: body.Keys.P256dh, Auth: body.Keys.Auth,
			UserID: pushCallerID(request), SessionID: pushCallerSession(request), Lang: normalizeLang(body.Lang), Asking: body.Asking, Waiting: body.Waiting,
		})
		if upsertErr != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", upsertErr.Error())
			return
		}
		responseWriter.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/push/subscriptions", func(responseWriter http.ResponseWriter, request *http.Request) {
		subscription, found, findErr := pushSubscriptionFind(request.URL.Query().Get("endpoint"))
		if findErr != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", findErr.Error())
			return
		}
		if !found || subscription.UserID != pushCallerID(request) {
			writeJSON(responseWriter, http.StatusOK, map[string]bool{"subscribed": false, "asking": false, "waiting": false})
			return
		}
		writeJSON(responseWriter, http.StatusOK, map[string]bool{"subscribed": true, "asking": subscription.Asking, "waiting": subscription.Waiting})
	})
	mux.HandleFunc("DELETE /v1/push/subscriptions", func(responseWriter http.ResponseWriter, request *http.Request) {
		endpoint := request.URL.Query().Get("endpoint")
		subscription, found, findErr := pushSubscriptionFind(endpoint)
		if findErr != nil {
			writeErr(responseWriter, http.StatusInternalServerError, "internal", findErr.Error())
			return
		}
		if found && subscription.UserID == pushCallerID(request) {
			if deleteErr := pushSubscriptionDelete(endpoint); deleteErr != nil {
				writeErr(responseWriter, http.StatusInternalServerError, "internal", deleteErr.Error())
				return
			}
		}
		responseWriter.WriteHeader(http.StatusNoContent)
	})
}
