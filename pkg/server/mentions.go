package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi"
)

func (srv *Server) handleListMentions(w http.ResponseWriter, r *http.Request) {
	var err error
	ctx := r.Context()
	var limit int64
	var offset int64
	rawLimit := r.URL.Query().Get("limit")
	rawOffset := r.URL.Query().Get("offset")
	if rawOffset != "" {
		offset, err = strconv.ParseInt(rawOffset, 10, 64)
		if err != nil {
			srv.sendError(ctx, w, err)
			return
		}
	}
	if rawLimit != "" {
		limit, err = strconv.ParseInt(rawLimit, 10, 64)
		if err != nil {
			srv.sendError(ctx, w, err)
			return
		}
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	tx, err := srv.cfg.Database.BeginTx(ctx, &sql.TxOptions{
		ReadOnly: true,
	})
	if err != nil {
		srv.sendError(ctx, w, err)
		return
	}
	defer tx.Rollback()
	query := "SELECT id, source, target, status, created_at FROM webmentions ORDER BY id ASC LIMIT ? OFFSET ?"
	result := PagedMentionList{
		Items: make([]Mention, 0, 10),
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(id) FROM webmentions").Scan(&result.Total); err != nil {
		srv.sendError(ctx, w, err)
		return
	}
	rows, err := tx.QueryContext(ctx, query, limit, offset)
	if err != nil {
		srv.sendError(ctx, w, err)
		return
	}
	for rows.Next() {
		m := Mention{}
		if err := rows.Scan(&m.ID, &m.Source, &m.Target, &m.Status, &m.CreatedAt); err != nil {
			srv.sendError(ctx, w, err)
			rows.Close()
			return
		}
		result.Items = append(result.Items, m)
	}
	rows.Close()
	// Now let's see, if there would be more items to fetch:
	rows, err = tx.QueryContext(ctx, "SELECT id, source, target, status, created_at FROM webmentions ORDER BY created_at DESC LIMIT ? OFFSET ?", 1, offset+limit)
	if err != nil {
		srv.sendError(ctx, w, err)
		return
	}
	defer rows.Close()
	if rows.Next() {
		v := url.Values{}
		v.Set("limit", r.URL.Query().Get("limit"))
		v.Set("offset", fmt.Sprintf("%d", offset+limit))
		u := url.URL{
			Scheme:   r.URL.Scheme,
			User:     r.URL.User,
			Path:     r.URL.Path,
			RawQuery: v.Encode(),
		}
		result.Next = u.String()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (srv *Server) handleApproveMention(w http.ResponseWriter, r *http.Request) {
	srv.handleMentionStatusUpdate(w, r, "approved")
}

func (srv *Server) handleMentionStatusUpdate(w http.ResponseWriter, r *http.Request, status string) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == "" {
		srv.sendError(ctx, w, &HTTPError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("no id provided"),
		})
		return
	}
	tx, err := srv.cfg.Database.BeginTx(ctx, &sql.TxOptions{
		ReadOnly: false,
	})
	if err != nil {
		srv.sendError(ctx, w, err)
		return
	}
	if _, err := tx.ExecContext(ctx, "UPDATE webmentions SET status = ? WHERE id = ?", status, id); err != nil {
		srv.sendError(ctx, w, err)
		tx.Rollback()
		return
	}
	if err := tx.Commit(); err != nil {
		srv.sendError(ctx, w, err)
		tx.Rollback()
		return
	}
}

func (srv *Server) handleRejectMention(w http.ResponseWriter, r *http.Request) {
	srv.handleMentionStatusUpdate(w, r, "rejected")
}
