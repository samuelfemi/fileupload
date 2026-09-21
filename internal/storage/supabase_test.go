package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewSupabaseStorage(t *testing.T) {
	s := NewSupabaseStorage("https://example.supabase.co/", "key123", "my-bucket")
	if s.BaseURL != "https://example.supabase.co" {
		t.Errorf("BaseURL = %q, want trimmed", s.BaseURL)
	}
	if s.APIKey != "key123" {
		t.Errorf("APIKey = %q", s.APIKey)
	}
	if s.Bucket != "my-bucket" {
		t.Errorf("Bucket = %q", s.Bucket)
	}
	if s.Client == nil {
		t.Error("Client should not be nil")
	}
	// without trailing slash
	s2 := NewSupabaseStorage("https://example.supabase.co", "k", "b")
	if s2.BaseURL != "https://example.supabase.co" {
		t.Errorf("BaseURL = %q", s2.BaseURL)
	}
}

func TestSupabaseStorage_ImplementsInterface(t *testing.T) {
	var _ Storage = &SupabaseStorage{}
	var _ Storage = (*SupabaseStorage)(nil)
}

func TestUpload_Success(t *testing.T) {
	var gotMethod, gotURL, gotAuth, gotApikey, gotCT string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotURL = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotApikey = r.Header.Get("apikey")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := NewSupabaseStorage(srv.URL, "secret-key", "files")
	s.Client = srv.Client()

	err := s.Upload(context.Background(), "user123/file-uuid", strings.NewReader("hello world"), "text/plain")
	if err != nil {
		t.Fatalf("Upload error: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	expectedPath := "/storage/v1/object/files/user123/file-uuid"
	if gotURL != expectedPath {
		t.Errorf("path = %q, want %q", gotURL, expectedPath)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotApikey != "secret-key" {
		t.Errorf("apikey = %q", gotApikey)
	}
	if gotCT != "text/plain" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotBody != "hello world" {
		t.Errorf("body = %q", gotBody)
	}
}

func TestUpload_FailureStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()
	s := NewSupabaseStorage(srv.URL, "k", "b")
	s.Client = srv.Client()
	err := s.Upload(context.Background(), "key", strings.NewReader("data"), "text/plain")
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if !strings.Contains(err.Error(), "status=400") {
		t.Errorf("error = %q, want status=400", err.Error())
	}
	if !strings.Contains(err.Error(), "supabase upload failed") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestUpload_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach server")
	}))
	defer srv.Close()
	s := NewSupabaseStorage(srv.URL, "k", "b")
	s.Client = srv.Client()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.Upload(ctx, "key", strings.NewReader("data"), "text/plain")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestUpload_NetworkError(t *testing.T) {
	s := NewSupabaseStorage("http://127.0.0.1:0", "k", "b")
	// Use default client which will fail to connect
	err := s.Upload(context.Background(), "key", strings.NewReader("data"), "text/plain")
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "upload file") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestDownload_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/storage/v1/object/files/user/key" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(200)
		w.Write([]byte("file content here"))
	}))
	defer srv.Close()
	s := NewSupabaseStorage(srv.URL, "k", "files")
	s.Client = srv.Client()
	rc, err := s.Download(context.Background(), "user/key")
	if err != nil {
		t.Fatalf("Download error: %v", err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "file content here" {
		t.Errorf("body = %q", string(b))
	}
}

func TestDownload_FailureStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`not found`))
	}))
	defer srv.Close()
	s := NewSupabaseStorage(srv.URL, "k", "b")
	s.Client = srv.Client()
	_, err := s.Download(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "status=404") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestDownload_NetworkError(t *testing.T) {
	s := NewSupabaseStorage("http://127.0.0.1:0", "k", "b")
	_, err := s.Download(context.Background(), "key")
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestDelete_Success(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(200)
	}))
	defer srv.Close()
	s := NewSupabaseStorage(srv.URL, "k", "mybucket")
	s.Client = srv.Client()
	err := s.Delete(context.Background(), "some/key")
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if gotMethod != "DELETE" {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/storage/v1/object/mybucket/some/key" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestDelete_FailureStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()
	s := NewSupabaseStorage(srv.URL, "k", "b")
	s.Client = srv.Client()
	err := s.Delete(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !strings.Contains(err.Error(), "status=500") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestDelete_ContextCancelled(t *testing.T) {
	s := NewSupabaseStorage("http://example.com", "k", "b")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Need a server, but cancelled context should fail before hitting server
	// Use a server that would panic if called
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach")
	}))
	defer srv.Close()
	s.Client = srv.Client()
	s.BaseURL = srv.URL
	err := s.Delete(ctx, "key")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// RoundTripFunc for mocking transport errors
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUpload_TransportError(t *testing.T) {
	s := NewSupabaseStorage("https://example.com", "k", "b")
	s.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("transport boom")
	})}
	err := s.Upload(context.Background(), "k", strings.NewReader("x"), "text/plain")
	if err == nil || !strings.Contains(err.Error(), "transport boom") {
		t.Errorf("error = %v, want transport boom", err)
	}
}
