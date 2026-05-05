package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

const (
	callbackPath           = "/callback"
	defaultCallbackTimeout = 2 * time.Minute
)

// CallbackResult holds the authorization code and state received from the
// OAuth provider after the user authorizes the application.
type CallbackResult struct {
	Code  string
	State string
}

// CallbackServer listens on a local port for the OAuth redirect and captures
// the authorization code. It shuts down automatically after receiving one
// successful callback or when the context is cancelled.
type CallbackServer struct {
	port   int
	server *http.Server
	result chan CallbackResult
	errCh  chan error
}

// NewCallbackServer creates a callback server bound to the given port.
// Use port 0 to let the OS pick a free port; call Port() after Start().
func NewCallbackServer(port int) *CallbackServer {
	cs := &CallbackServer{
		port:   port,
		result: make(chan CallbackResult, 1),
		errCh:  make(chan error, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, cs.handleCallback)

	cs.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	return cs
}

// Start begins listening. It returns the actual bound port (useful when port 0
// was requested) and any bind error.
func (cs *CallbackServer) Start() (int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cs.port))
	if err != nil {
		return 0, fmt.Errorf("oauth callback: bind port %d: %w", cs.port, err)
	}
	cs.port = ln.Addr().(*net.TCPAddr).Port

	go func() {
		if err := cs.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			select {
			case cs.errCh <- err:
			default:
			}
		}
	}()

	return cs.port, nil
}

// Wait blocks until the callback is received, the context is cancelled, or the
// timeout elapses. It shuts down the server before returning.
func (cs *CallbackServer) Wait(ctx context.Context) (*CallbackResult, error) {
	defer cs.shutdown()

	timer := time.NewTimer(defaultCallbackTimeout)
	defer timer.Stop()

	select {
	case result := <-cs.result:
		return &result, nil
	case err := <-cs.errCh:
		return nil, fmt.Errorf("oauth callback server error: %w", err)
	case <-timer.C:
		return nil, ErrCallbackTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Port returns the port the server is (or will be) listening on.
func (cs *CallbackServer) Port() int {
	return cs.port
}

func (cs *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	if errParam := q.Get("error"); errParam != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(errorHTML(errParam))) //nolint:errcheck
		select {
		case cs.errCh <- fmt.Errorf("oauth provider error: %s", errParam):
		default:
		}
		return
	}

	code := q.Get("code")
	state := q.Get("state")
	if code == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("missing authorization code")) //nolint:errcheck
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(successHTML())) //nolint:errcheck

	select {
	case cs.result <- CallbackResult{Code: code, State: state}:
	default:
	}
}

func (cs *CallbackServer) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cs.server.Shutdown(ctx) //nolint:errcheck
}

// TryCallbackPorts attempts to start a callback server on the preferred port,
// falling back to a list of alternatives if the preferred port is busy.
func TryCallbackPorts(preferred int, fallbacks ...int) (*CallbackServer, int, error) {
	ports := append([]int{preferred}, fallbacks...)
	for _, port := range ports {
		cs := NewCallbackServer(port)
		actualPort, err := cs.Start()
		if err == nil {
			return cs, actualPort, nil
		}
	}
	return nil, 0, fmt.Errorf("oauth callback: no available port in %v", ports)
}

func successHTML() string {
	return `<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>Authentication Successful</title>
<style>
body{font-family:system-ui,sans-serif;background:#0f172a;color:#f8fafc;
     display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
.card{background:#1e293b;padding:3rem;border-radius:1rem;text-align:center;
      max-width:400px;border:1px solid #334155}
h1{color:#a78bfa;margin:0 0 1rem}p{color:#94a3b8}
</style></head>
<body>
<div class="card">
  <h1>&#10003; Authentication Successful</h1>
  <p>You can close this window and return to the dashboard.</p>
</div>
<script>setTimeout(()=>window.close(),3000)</script>
</body></html>`
}

func errorHTML(errMsg string) string {
	return `<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>Authentication Failed</title>
<style>
body{font-family:system-ui,sans-serif;background:#0f172a;color:#f8fafc;
     display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
.card{background:#1e293b;padding:3rem;border-radius:1rem;text-align:center;
      max-width:400px;border:1px solid #334155}
h1{color:#ef4444;margin:0 0 1rem}p{color:#94a3b8}
.err{background:rgba(239,68,68,.1);padding:1rem;border-radius:.5rem;
     color:#fca5a5;font-family:monospace;font-size:.9rem;margin-top:1rem}
</style></head>
<body>
<div class="card">
  <h1>&#10007; Authentication Failed</h1>
  <p>Please close this window and try again.</p>
  <div class="err">` + errMsg + `</div>
</div>
</body></html>`
}
