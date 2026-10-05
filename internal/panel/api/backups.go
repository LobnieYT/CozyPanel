package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"cozy/internal/panel/auth"
	"cozy/internal/panel/backup"
	"cozy/internal/panel/secure"
)

// staging holds uploaded archives between upload, preview and apply.
type staging struct {
	mu    sync.Mutex
	dirs  map[string]*backup.Staged
	since map[string]time.Time
}

func newStaging() *staging {
	return &staging{dirs: map[string]*backup.Staged{}, since: map[string]time.Time{}}
}

func stagingID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *staging) put(st *backup.Staged) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, at := range s.since {
		if now.Sub(at) > 24*time.Hour {
			if old, ok := s.dirs[id]; ok {
				_ = old.Close()
			}
			delete(s.dirs, id)
			delete(s.since, id)
		}
	}
	id := stagingID()
	s.dirs[id] = st
	s.since[id] = now
	return id
}

func (s *staging) get(id string) *backup.Staged {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirs[id]
}

func (s *staging) drop(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.dirs[id]; ok {
		_ = st.Close()
	}
	delete(s.dirs, id)
	delete(s.since, id)
}

// ownerSession authenticates a raw (non-huma) handler like the middleware
// does, then requires the owner: backups carry every secret of the server.
func (h *handlers) ownerSession(w http.ResponseWriter, r *http.Request) bool {
	write := func(code int, text string) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"title":"` + text + `","status":` + strconv.Itoa(code) + `,"detail":"` + text + `"}`))
	}
	ck, err := r.Cookie(auth.CookieName)
	if err != nil {
		write(http.StatusUnauthorized, "unauthorized")
		return false
	}
	sess, err := h.d.Sessions.Lookup(r.Context(), ck.Value)
	if err != nil {
		write(http.StatusUnauthorized, "unauthorized")
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if !secure.Equal(r.Header.Get("X-CSRF-Token"), sess.CsrfToken) {
			write(http.StatusForbidden, "csrf")
			return false
		}
	}
	admin, err := h.d.Store.Q.GetAdmin(r.Context(), sess.AdminID)
	if err != nil {
		write(http.StatusUnauthorized, "unauthorized")
		return false
	}
	if admin.IsOwner == 0 {
		write(http.StatusForbidden, "no_grant")
		return false
	}
	if err := AdminStatus(admin, h.d.Now()); err != nil {
		write(http.StatusForbidden, "account_closed")
		return false
	}
	return true
}

func (h *handlers) registerBackups(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/backups/export", h.backupExport)
	mux.HandleFunc("POST /api/v1/backups/upload", h.backupUpload)
	mux.HandleFunc("GET /api/v1/backups/preview", h.backupPreview)
	mux.HandleFunc("POST /api/v1/backups/apply", h.backupApply)
}

func (h *handlers) backupExport(w http.ResponseWriter, r *http.Request) {
	if !h.ownerSession(w, r) {
		return
	}
	path, name, err := backup.Export(r.Context(), h.d.Store, h.d.DataDir, h.d.Version)
	if err != nil {
		h.d.Log.Error("backup export", "err", err)
		writeProblem(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer os.Remove(path)
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, path)
	h.audit(r.Context(), 0, "backup.export", "", "", map[string]any{"file": name})
}

func writeProblem(w http.ResponseWriter, code int, text string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"title":"` + text + `","status":` + strconv.Itoa(code) + `,"detail":"` + text + `"}`))
}

func (h *handlers) backupUpload(w http.ResponseWriter, r *http.Request) {
	if !h.ownerSession(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, backup.MaxArchive+16<<20)
	st, err := backup.OpenArchive(r.Body, backup.MaxArchive)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, backupError(err))
		return
	}
	id := h.staging.put(st)
	writeJSON(w, map[string]any{"id": id, "manifest": st.Manifest})
	h.audit(r.Context(), 0, "backup.upload", "", "", nil)
}

func backupError(err error) string {
	switch err {
	case backup.ErrFormat:
		return "bad_backup"
	case backup.ErrTooBig:
		return "backup_too_big"
	case backup.ErrSlip:
		return "bad_backup"
	}
	return "bad_backup"
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (h *handlers) backupPreview(w http.ResponseWriter, r *http.Request) {
	if !h.ownerSession(w, r) {
		return
	}
	st := h.staging.get(r.URL.Query().Get("id"))
	if st == nil {
		writeProblem(w, http.StatusNotFound, "not_found")
		return
	}
	pv, err := backup.Inspect(r.Context(), h.d.Store, st)
	if err != nil {
		h.d.Log.Error("backup preview", "err", err)
		writeProblem(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, pv)
}

func (h *handlers) backupApply(w http.ResponseWriter, r *http.Request) {
	if !h.ownerSession(w, r) {
		return
	}
	var in struct {
		ID       string   `json:"id"`
		Sections []string `json:"sections"`
		Strategy string   `json:"strategy"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID == "" {
		writeProblem(w, http.StatusUnprocessableEntity, "bad_request")
		return
	}
	st := h.staging.get(in.ID)
	if st == nil {
		writeProblem(w, http.StatusNotFound, "not_found")
		return
	}
	out, err := backup.Apply(r.Context(), h.d.Store, h.d.DataDir, st, backup.ApplyRequest{
		Sections: in.Sections, Strategy: backup.Strategy(in.Strategy),
	}, h.d.Version)
	if err != nil {
		h.d.Log.Error("backup apply", "err", err)
		writeProblem(w, http.StatusUnprocessableEntity, "invalid")
		return
	}
	h.staging.drop(in.ID)
	h.d.Changes.SlotsChanged()
	h.audit(r.Context(), 0, "backup.apply", "", "", map[string]any{"sections": in.Sections, "strategy": in.Strategy})
	writeJSON(w, out)
}
