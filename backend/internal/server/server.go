package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"rapidassistant/backend/internal/config"
	"rapidassistant/backend/internal/fathom"
	"rapidassistant/backend/internal/files"
	"rapidassistant/backend/internal/models"
	"rapidassistant/backend/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Server struct {
	cfg     config.Config
	store   *store.PostgresStore
	storage *files.LocalStorage
	fathom  *fathom.Client
	router  http.Handler
}

func New(cfg config.Config, store *store.PostgresStore, storage *files.LocalStorage) *Server {
	s := &Server{
		cfg:     cfg,
		store:   store,
		storage: storage,
		fathom:  fathom.NewClient(cfg.FathomAPIKey),
	}
	s.router = s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.cors)
	r.Get("/healthz", s.handleHealth)

	r.Route("/v1", func(r chi.Router) {
		r.Post("/meetings", s.handleCreateMeeting)
		r.Post("/meetings/{meetingID}/submit", s.handleSubmitMeeting)
		r.Get("/meetings/{meetingID}", s.handleGetMeeting)
		r.Get("/meetings/{meetingID}/draft", s.handleGetDraft)
		r.Patch("/meetings/{meetingID}/review", s.handleSaveReview)
		r.Post("/meetings/{meetingID}/finalize", s.handleFinalize)
		r.Get("/meetings/{meetingID}/report", s.handleGetReport)

		r.Route("/integrations/fathom", func(r chi.Router) {
			r.Post("/import", s.handleFathomImport)
			r.Post("/webhook", s.handleFathomWebhook)
		})
	})

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateMeeting(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.EnableAudioUploads {
		writeError(w, http.StatusServiceUnavailable, "audio uploads are disabled in this environment", nil)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes)
	if err := r.ParseMultipartForm(s.cfg.MaxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form", err)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required", err)
		return
	}
	defer file.Close()

	if title == "" {
		title = strings.TrimSuffix(fileHeader.Filename, filepath.Ext(fileHeader.Filename))
	}

	if !isAllowedAudioFile(fileHeader.Filename) {
		writeError(w, http.StatusBadRequest, "unsupported file type", nil)
		return
	}

	meetingID := uuid.NewString()
	saved, err := s.storage.SaveUpload(r.Context(), meetingID, fileHeader.Filename, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save upload", err)
		return
	}

	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(fileHeader.Filename))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	meeting, err := s.store.CreateMeeting(r.Context(), meetingID, title, fileHeader.Filename, contentType, saved.SizeBytes, saved.RelativePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create meeting", err)
		return
	}

	writeJSON(w, http.StatusCreated, createMeetingResponse(meeting.ID))
}

func (s *Server) handleFathomImport(w http.ResponseWriter, r *http.Request) {
	var request models.ImportFathomRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid Fathom import payload", err)
		return
	}

	input, err := s.fathom.FetchRecordingByReference(r.Context(), request.RecordingID, request.ShareURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "fetch Fathom recording", err)
		return
	}

	meeting, _, err := s.store.ImportFathomMeeting(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "import Fathom meeting", err)
		return
	}

	writeJSON(w, http.StatusCreated, createMeetingResponse(meeting.ID))
}

func (s *Server) handleFathomWebhook(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(s.cfg.FathomWebhookSecret) == "" {
		writeError(w, http.StatusServiceUnavailable, "Fathom webhook secret is not configured", nil)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 5<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read webhook payload", err)
		return
	}

	if err := fathom.VerifyWebhook(
		s.cfg.FathomWebhookSecret,
		r.Header.Get("webhook-id"),
		r.Header.Get("webhook-timestamp"),
		r.Header.Get("webhook-signature"),
		body,
		time.Duration(s.cfg.FathomWebhookToleranceSecs)*time.Second,
	); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid Fathom webhook signature", err)
		return
	}

	input, err := fathom.ParseWebhookPayload(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "decode Fathom webhook payload", err)
		return
	}
	input.ProviderEventID = strings.TrimSpace(r.Header.Get("webhook-id"))
	input.ProviderPayloadJSON = string(body)

	if input.RecordingID == "" {
		writeError(w, http.StatusBadRequest, "Fathom webhook missing recording_id", nil)
		return
	}
	if len(input.Segments) == 0 || input.SummaryMarkdown == nil {
		fetched, fetchErr := s.fathom.FetchRecording(r.Context(), input.RecordingID)
		if fetchErr != nil {
			writeError(w, http.StatusBadGateway, "hydrate Fathom webhook payload", fetchErr)
			return
		}
		input = mergeFathomImportInputs(input, fetched)
	}

	meeting, duplicate, err := s.store.ImportFathomMeeting(r.Context(), input)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) && duplicate {
			writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "duplicate": true})
			return
		}
		writeError(w, http.StatusInternalServerError, "import Fathom webhook meeting", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"duplicate":  duplicate,
		"meeting_id": meeting.ID,
	})
}

func (s *Server) handleSubmitMeeting(w http.ResponseWriter, r *http.Request) {
	meetingID := chi.URLParam(r, "meetingID")
	if err := s.store.QueueMeetingProcessing(r.Context(), meetingID); err != nil {
		writeStoreError(w, err)
		return
	}

	meeting, err := s.store.GetMeeting(r.Context(), meetingID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, meeting)
}

func (s *Server) handleGetMeeting(w http.ResponseWriter, r *http.Request) {
	meetingID := chi.URLParam(r, "meetingID")
	meeting, err := s.store.GetMeeting(r.Context(), meetingID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meeting)
}

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	meetingID := chi.URLParam(r, "meetingID")
	sessionID := r.Header.Get(s.cfg.ReviewSessionHeader)
	draft, err := s.store.GetDraft(r.Context(), meetingID, sessionID, time.Duration(s.cfg.ReviewLockTTLMinutes)*time.Minute)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) handleSaveReview(w http.ResponseWriter, r *http.Request) {
	meetingID := chi.URLParam(r, "meetingID")
	sessionID := strings.TrimSpace(r.Header.Get(s.cfg.ReviewSessionHeader))
	if sessionID == "" {
		writeError(w, http.StatusConflict, "missing review session", nil)
		return
	}

	var request models.SaveReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid review payload", err)
		return
	}

	reviewVersion, err := s.store.SaveReview(r.Context(), meetingID, sessionID, request.Speakers, request.Segments)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, models.SaveReviewResponse{
		MeetingID:     meetingID,
		ReviewVersion: reviewVersion,
	})
}

func (s *Server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	meetingID := chi.URLParam(r, "meetingID")
	sessionID := strings.TrimSpace(r.Header.Get(s.cfg.ReviewSessionHeader))
	if sessionID == "" {
		writeError(w, http.StatusConflict, "missing review session", nil)
		return
	}

	if err := s.store.EnqueueReportGeneration(r.Context(), meetingID, sessionID); err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, models.FinalizeResponse{
		MeetingID: meetingID,
		Status:    models.StatusReportGenerating,
	})
}

func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	meetingID := chi.URLParam(r, "meetingID")
	report, err := s.store.GetReport(r.Context(), meetingID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.cfg.FrontendOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Review-Session")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func createMeetingResponse(meetingID string) models.CreateMeetingResponse {
	return models.CreateMeetingResponse{
		MeetingID: meetingID,
		SubmitURL: "/v1/meetings/" + meetingID + "/submit",
		ReviewURL: "/v1/meetings/" + meetingID + "/draft",
		StatusURL: "/v1/meetings/" + meetingID,
		ReportURL: "/v1/meetings/" + meetingID + "/report",
	}
}

func mergeFathomImportInputs(primary, fallback models.FathomImportInput) models.FathomImportInput {
	merged := fallback
	if strings.TrimSpace(primary.ProviderEventID) != "" {
		merged.ProviderEventID = primary.ProviderEventID
	}
	if strings.TrimSpace(primary.ProviderPayloadJSON) != "" {
		merged.ProviderPayloadJSON = primary.ProviderPayloadJSON
	}
	if strings.TrimSpace(primary.RecordingID) != "" {
		merged.RecordingID = primary.RecordingID
	}
	if strings.TrimSpace(primary.Title) != "" {
		merged.Title = primary.Title
	}
	if primary.MeetingTitle != nil {
		merged.MeetingTitle = primary.MeetingTitle
	}
	if primary.URL != nil {
		merged.URL = primary.URL
	}
	if primary.ShareURL != nil {
		merged.ShareURL = primary.ShareURL
	}
	if primary.RecordedByName != nil {
		merged.RecordedByName = primary.RecordedByName
	}
	if primary.TranscriptLanguage != nil {
		merged.TranscriptLanguage = primary.TranscriptLanguage
	}
	if primary.SummaryMarkdown != nil {
		merged.SummaryMarkdown = primary.SummaryMarkdown
	}
	if primary.ActionItemsJSON != nil {
		merged.ActionItemsJSON = primary.ActionItemsJSON
	}
	if primary.ExternalCreatedAt != nil {
		merged.ExternalCreatedAt = primary.ExternalCreatedAt
	}
	if primary.DraftPreviewJSON != nil {
		merged.DraftPreviewJSON = primary.DraftPreviewJSON
	}
	if len(primary.Segments) > 0 {
		merged.Segments = primary.Segments
	}
	return merged
}

func isAllowedAudioFile(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".wav", ".mp3", ".m4a":
		return true
	default:
		return false
	}
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "record not found", err)
	case errors.Is(err, store.ErrReviewLocked):
		writeError(w, http.StatusConflict, "meeting is being reviewed in another session", err)
	default:
		writeError(w, http.StatusInternalServerError, "internal server error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string, err error) {
	if err != nil {
		log.Printf("http error: %s: %v", message, err)
	}
	writeJSON(w, status, map[string]any{
		"error":   message,
		"details": errorDetails(err),
	})
}

func errorDetails(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("encode response: %v", err)
	}
}
