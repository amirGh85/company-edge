package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"io"
	"net"
	"net/url"
	"regexp"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const sessionCookie = "company_edge_session"
const sessionLifetime = 8 * time.Hour

type HealthResponse struct {
	Status    string `json:"status"`
	Service   string `json:"service"`
	Mode      string `json:"mode"`
	Timestamp string `json:"timestamp"`
}

type Session struct {
	User    string
	Expires time.Time
}

type Auth struct {
	User     string
	Password string
	mu       sync.Mutex
	sessions map[string]Session
}

type DomainInput struct {
    Domain string `json:"domain"`
    PrimaryOrigin string `json:"primary_origin"`
    BackupOrigin string `json:"backup_origin"`
    PrimaryHealth string `json:"primary_health"`
    BackupHealth string `json:"backup_health"`
}

type DomainRecord struct {
    ID string `json:"id"`
    Domain string `json:"domain"`
    Route string `json:"route"`
    PrimaryOrigin string `json:"primary_origin"`
    BackupOrigin string `json:"backup_origin"`
    PrimaryHealth string `json:"primary_health,omitempty"`
    BackupHealth string `json:"backup_health,omitempty"`
    Status string `json:"status"`
    CreatedAt string `json:"created_at"`
    CreatedBy string `json:"created_by"`
}

type AuditEvent struct {
    Time string `json:"time"`
    Actor string `json:"actor"`
    Action string `json:"action"`
    Domain string `json:"domain,omitempty"`
    Result string `json:"result"`
}

type PersistentStore struct {
    Domains []DomainRecord `json:"domains"`
    Audit []AuditEvent `json:"audit"`
}

var domainLabelRE = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func validatePublicDomain(value string) bool {
    value = strings.TrimSuffix(strings.TrimSpace(value), ".")
    if value == "" || len(value) > 253 || !strings.Contains(value, ".") || net.ParseIP(value) != nil {
        return false
    }
    lower := strings.ToLower(value)
    if strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".local") ||
        strings.HasSuffix(lower, ".internal") || strings.HasSuffix(lower, ".test") {
        return false
    }
    for _, label := range strings.Split(value, ".") {
        if len(label) == 0 || len(label) > 63 || !domainLabelRE.MatchString(label) {
            return false
        }
    }
    return true
}

func validateOrigin(value string) bool {
    u, err := url.Parse(strings.TrimSpace(value))
    if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
        u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
        return false
    }
    if u.Path != "" && u.Path != "/" {
        return false
    }
    host := u.Hostname()
    if host == "" || strings.ContainsAny(host, " \t\r\n") {
        return false
    }
    if port := u.Port(); port != "" {
        n := 0
        for _, c := range port {
            if c < '0' || c > '9' { return false }
            n = n*10 + int(c-'0')
            if n > 65535 { return false }
        }
        if n < 1 { return false }
    }
    if net.ParseIP(host) != nil {
        return true
    }
    if len(host) > 253 {
        return false
    }
    for _, label := range strings.Split(host, ".") {
        if len(label) == 0 || len(label) > 63 || !domainLabelRE.MatchString(label) {
            return false
        }
    }
    return true
}

func validateHealthURL(value string) bool {
    u, err := url.Parse(strings.TrimSpace(value))
    if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
        u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
        return false
    }
    if !validateOrigin(u.Scheme + "://" + u.Host) {
        return false
    }
    return !strings.ContainsAny(u.Path, "\\r\\n")
}

type StoreManager struct {
    mu sync.Mutex
    path string
}

func (m *StoreManager) loadLocked() (PersistentStore, error) {
    store := PersistentStore{
        Domains: []DomainRecord{},
        Audit: []AuditEvent{},
    }
    data, err := os.ReadFile(m.path)
    if errors.Is(err, os.ErrNotExist) {
        return store, nil
    }
    if err != nil {
        return store, err
    }
    if err := json.Unmarshal(data, &store); err != nil {
        return PersistentStore{}, err
    }
    if store.Domains == nil {
        store.Domains = []DomainRecord{}
    }
    if store.Audit == nil {
        store.Audit = []AuditEvent{}
    }
    return store, nil
}

func (m *StoreManager) saveLocked(store PersistentStore) error {
    if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
        return err
    }
    data, err := json.MarshalIndent(store, "", "  ")
    if err != nil {
        return err
    }
    f, err := os.CreateTemp(filepath.Dir(m.path), ".store-*.tmp")
    if err != nil {
        return err
    }
    name := f.Name()
    defer os.Remove(name)
    if err := f.Chmod(0600); err != nil {
        f.Close()
        return err
    }
    if _, err := f.Write(data); err != nil {
        f.Close()
        return err
    }
    if err := f.Sync(); err != nil {
        f.Close()
        return err
    }
    if err := f.Close(); err != nil {
        return err
    }
    return os.Rename(name, m.path)
}

func main() {
	addr := os.Getenv("CONTROL_PANEL_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8090"
	}

	adminUser := os.Getenv("CONTROL_PANEL_ADMIN_USER")
	adminPassword := os.Getenv("CONTROL_PANEL_ADMIN_PASSWORD")
	if adminUser == "" || adminPassword == "" {
		log.Fatal("CONTROL_PANEL_ADMIN_USER and CONTROL_PANEL_ADMIN_PASSWORD must be configured")
	}
	if len(adminPassword) < 14 {
		log.Fatal("CONTROL_PANEL_ADMIN_PASSWORD must be at least 14 characters")
	}

	staticDir := filepath.Join("control-panel", "static")
	if _, err := os.Stat(filepath.Join(staticDir, "index.html")); err != nil {
		log.Fatalf("UI file unavailable: %v", err)
	}

	auth := &Auth{
		User:     adminUser,
		Password: adminPassword,
		sessions: make(map[string]Session),
	}

	storeManager := &StoreManager{path: "/app/data/store.json"}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(HealthResponse{
			Status:    "ok",
			Service:   "company-edge-control-panel",
			Mode:      "authenticated-preview",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	})


	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		type Entry struct {
			Route string `json:"route" yaml:"route"`
			Dest string `json:"dest" yaml:"dest"`
			Ping string `json:"ping,omitempty" yaml:"ping"`
		}
		data, err := os.ReadFile("/app/reproxy.yml")
		if err != nil {
			http.Error(w, "Configuration unavailable", http.StatusServiceUnavailable)
			return
		}
		var config map[string][]Entry
		if err := yaml.Unmarshal(data, &config); err != nil {
			log.Printf("config parse failed: %v", err)
			http.Error(w, "Configuration could not be parsed", http.StatusInternalServerError)
			return
		}
		type RouteInfo struct {
			Group string `json:"group"`
			Route string `json:"route"`
			Dest string `json:"dest"`
			Ping string `json:"ping,omitempty"`
		}
		routes := make([]RouteInfo, 0)
		for group, entries := range config {
			for _, entry := range entries {
				routes = append(routes, RouteInfo{Group: group, Route: entry.Route, Dest: entry.Dest, Ping: entry.Ping})
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"timestamp": time.Now().UTC().Format(time.RFC3339), "routes": routes})
	})

	mux.HandleFunc("GET /api/monitor", func(w http.ResponseWriter, r *http.Request) {
		type Probe struct {
			Name       string  `json:"name"`
			URL        string  `json:"url"`
			Status     string  `json:"status"`
			HTTPStatus int     `json:"http_status,omitempty"`
			LatencyMS  float64 `json:"latency_ms"`
			Error      string  `json:"error,omitempty"`
		}

		targets := []struct {
			name string
			url  string
		}{
			{"Reproxy", "http://company-reproxy:8080/"},
			{"Origin-primary", "http://host.docker.internal:3000/"},
			{"Origin-backup", "http://host.docker.internal:3001/"},
		}

		client := &http.Client{
			Timeout: 3 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		results := make([]Probe, 0, len(targets))
		for _, target := range targets {
			result := Probe{Name: target.name, URL: target.url, Status: "down"}
			start := time.Now()
			resp, err := client.Get(target.url)
			result.LatencyMS = float64(time.Since(start).Microseconds()) / 1000
			if err != nil {
				result.Error = "request failed"
			} else {
				result.HTTPStatus = resp.StatusCode
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 400 {
					result.Status = "up"
				} else {
					result.Error = "unexpected HTTP status"
				}
			}
			results = append(results, result)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"services":  results,
		})
	})

	mux.HandleFunc("GET /api/domains", func(w http.ResponseWriter, r *http.Request) {
		storeManager.mu.Lock()
		store, err := storeManager.loadLocked()
		storeManager.mu.Unlock()
		if err != nil {
			http.Error(w, "Domain store unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"domains": store.Domains})
	})

	mux.HandleFunc("GET /api/audit", func(w http.ResponseWriter, r *http.Request) {
		storeManager.mu.Lock()
		store, err := storeManager.loadLocked()
		storeManager.mu.Unlock()
		if err != nil {
			http.Error(w, "Audit store unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"events": store.Audit})
	})

	mux.HandleFunc("POST /api/domains", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Request body too large or unreadable", http.StatusBadRequest)
			return
		}
		var input DomainInput
		if err := json.Unmarshal(body, &input); err != nil {
			http.Error(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}
		input.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(input.Domain), "."))
		input.PrimaryOrigin = strings.TrimSpace(input.PrimaryOrigin)
		input.BackupOrigin = strings.TrimSpace(input.BackupOrigin)
		input.PrimaryHealth = strings.TrimSpace(input.PrimaryHealth)
		input.BackupHealth = strings.TrimSpace(input.BackupHealth)

		if !validatePublicDomain(input.Domain) ||
			!validateOrigin(input.PrimaryOrigin) ||
			!validateOrigin(input.BackupOrigin) ||
			(input.PrimaryHealth != "" && !validateHealthURL(input.PrimaryHealth)) ||
			(input.BackupHealth != "" && !validateHealthURL(input.BackupHealth)) {
			http.Error(w, "Invalid domain, origin, or health-check URL", http.StatusBadRequest)
			return
		}
		session, err := auth.validSession(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		storeManager.mu.Lock()
		defer storeManager.mu.Unlock()
		store, err := storeManager.loadLocked()
		if err != nil {
			http.Error(w, "Domain store unavailable", http.StatusInternalServerError)
			return
		}
		if len(store.Domains) >= 500 {
			http.Error(w, "Domain draft limit reached", http.StatusConflict)
			return
		}
		for _, existing := range store.Domains {
			if strings.EqualFold(existing.Domain, input.Domain) {
				http.Error(w, "Domain already exists", http.StatusConflict)
				return
			}
		}
		idBytes := make([]byte, 16)
		if _, err := rand.Read(idBytes); err != nil {
			http.Error(w, "Could not create domain ID", http.StatusInternalServerError)
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		record := DomainRecord{
			ID: hex.EncodeToString(idBytes), Domain: input.Domain, Route: "/",
			PrimaryOrigin: input.PrimaryOrigin, BackupOrigin: input.BackupOrigin,
			PrimaryHealth: input.PrimaryHealth, BackupHealth: input.BackupHealth,
			Status: "draft", CreatedAt: now, CreatedBy: session.User,
		}
		store.Domains = append(store.Domains, record)
		store.Audit = append(store.Audit, AuditEvent{
			Time: now, Actor: session.User, Action: "domain.create",
			Domain: input.Domain, Result: "draft_saved",
		})
		if len(store.Audit) > 1000 {
			store.Audit = store.Audit[len(store.Audit)-1000:]
		}
		if err := storeManager.saveLocked(store); err != nil {
			http.Error(w, "Could not save domain draft", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(record)
	})

	mux.HandleFunc("GET /login", loginPage)
	mux.HandleFunc("POST /login", auth.login)
	mux.HandleFunc("POST /logout", auth.logout)
	mux.Handle("GET /", http.FileServer(http.Dir(staticDir)))

	server := &http.Server{
		Addr:              addr,
		Handler:           securityHeaders(auth.protect(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Control panel listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}

func loginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Company Edge | Sign in</title>
<style>
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0a0e14;color:#e7edf5;font:14px system-ui,sans-serif}
main{width:min(390px,calc(100% - 32px));padding:28px;background:#111823;border:1px solid #263244;border-radius:12px}
.logo{color:#55d6e8;font-weight:800;letter-spacing:1px}h1{font-size:22px;margin:22px 0 5px}p{color:#8c9bb0;font-size:12px;margin:0 0 24px}
label{display:block;margin:15px 0 7px;color:#b7c4d5;font-size:12px}
input{width:100%;padding:11px;border:1px solid #334257;border-radius:7px;background:#0a111a;color:#fff;font:inherit}
button{width:100%;margin-top:22px;padding:11px;border:1px solid #23616d;border-radius:7px;background:#16404a;color:#baf6ff;font:inherit;cursor:pointer}
.note{margin-top:18px;color:#65758a;font-size:11px}
</style></head><body><main><div class="logo">COMPANY EDGE</div>
<h1>Sign in</h1><p>Internal CDN control panel</p>
<form method="post" action="/login" autocomplete="on">
<label for="username">Username</label><input id="username" name="username" autocomplete="username" required>
<label for="password">Password</label><input id="password" name="password" type="password" autocomplete="current-password" required>
<button type="submit">Sign in</button></form>
<div class="note">Authorized administrators only.</div></main></body></html>`))
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	user := r.FormValue("username")
	password := r.FormValue("password")
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(a.User)) == 1
	passwordOK := subtle.ConstantTimeCompare([]byte(password), []byte(a.Password)) == 1
	if !userOK || !passwordOK {
		time.Sleep(700 * time.Millisecond)
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, "Unable to create session", http.StatusInternalServerError)
		return
	}
	id := hex.EncodeToString(raw)
	expires := time.Now().Add(sessionLifetime)

	a.mu.Lock()
	a.sessions[id] = Session{User: a.User, Expires: expires}
	a.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    id,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *Auth) validSession(r *http.Request) (Session, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return Session{}, errors.New("missing session")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	session, ok := a.sessions[cookie.Value]
	if !ok {
		return Session{}, errors.New("invalid session")
	}
	if time.Now().After(session.Expires) {
		delete(a.sessions, cookie.Value)
		return Session{}, errors.New("expired session")
	}
	return session, nil
}

func (a *Auth) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" ||
			r.URL.Path == "/login" {
			next.ServeHTTP(w, r)
			return
		}

		_, err := a.validSession(r)
		if err != nil {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
