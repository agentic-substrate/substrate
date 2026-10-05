package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/agentic-substrate/substrate/internal/node"
	"github.com/agentic-substrate/substrate/internal/strictjson"
)

func decodeBrowserBody(r *http.Request, value any) bool {
	typeName, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typeName != "application/json" {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024+1))
	if err != nil || len(data) > 2*1024*1024 || !strictjson.ValidText(data) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return false
	}
	return decoder.Decode(new(any)) == io.EOF
}

func browserError(w http.ResponseWriter, response node.Response) {
	status := http.StatusBadRequest
	switch response.Code {
	case "denied":
		status = http.StatusForbidden
	case "conflict":
		status = http.StatusConflict
	case "unavailable":
		status = http.StatusServiceUnavailable
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func (options Options) browser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if !options.allowedRequest(r) || r.Method == http.MethodPost && (len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != "http://"+options.Host) {
		browserError(w, node.Response{Error: "Request origin or host denied.", Code: "denied"})
		return
	}
	credentials := r.Header.Values("Authorization")
	if len(credentials) != 1 || !strings.HasPrefix(credentials[0], "Bearer ") {
		browserError(w, node.Response{Error: "A credential issued through trusted local setup is required.", Code: "denied"})
		return
	}
	request := node.Request{Token: strings.TrimPrefix(credentials[0], "Bearer "), ID: r.PathValue("id")}
	if len(request.ID) > 128 {
		browserError(w, node.Response{Error: "Invalid artifact or proposal identifier.", Code: "invalid"})
		return
	}
	switch r.Pattern {
	case "GET /api/artifacts":
		request.Action = "browser-inventory"
	case "GET /api/artifacts/{id}":
		request.Action = "browser-inspect"
	case "GET /api/publications/{id}":
		request.Action = "publication-get"
	case "POST /api/publications":
		request.Action = "publication-propose"
		if !decodeBrowserBody(r, &request.Publication) {
			browserError(w, node.Response{Error: "Use one bounded UTF-8 JSON publication request with supported fields.", Code: "invalid"})
			return
		}
	case "GET /api/publication-review":
		request.Action = "publication-review"
	case "POST /api/publication-review":
		request.Action = "publication-publish"
		if !decodeBrowserBody(r, &request.PublicationApproval) {
			browserError(w, node.Response{Error: "Use one bounded UTF-8 JSON exact review request with supported fields.", Code: "invalid"})
			return
		}
	}
	response := node.Call(r.Context(), options.Authority.Dir, request)
	if response.Error != "" {
		browserError(w, response)
		return
	}
	_, _ = w.Write(response.Result)
}
