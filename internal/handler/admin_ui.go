package handler

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AdminUIHandler serves the embedded admin UI
type AdminUIHandler struct {
	staticFS   http.FileSystem
	indexHTML  []byte
	fileServer http.Handler
}

// NewAdminUIHandler creates a new admin UI handler
// webFS should be the embedded filesystem containing the built frontend
func NewAdminUIHandler(webFS embed.FS, subDir string) (*AdminUIHandler, error) {
	// Get the subdirectory containing the built files
	subFS, err := fs.Sub(webFS, subDir)
	if err != nil {
		return nil, err
	}

	staticFS := http.FS(subFS)

	// Read index.html for SPA fallback
	indexFile, err := subFS.Open("index.html")
	if err != nil {
		return nil, err
	}
	defer indexFile.Close()

	stat, err := indexFile.Stat()
	if err != nil {
		return nil, err
	}

	indexHTML := make([]byte, stat.Size())
	_, err = indexFile.Read(indexHTML)
	if err != nil {
		return nil, err
	}

	return &AdminUIHandler{
		staticFS:   staticFS,
		indexHTML:  indexHTML,
		fileServer: http.FileServer(staticFS),
	}, nil
}

// ServeHTTP implements http.Handler for the admin UI
func (h *AdminUIHandler) ServeHTTP(c *gin.Context) {
	path := c.Request.URL.Path

	// Strip the /admin prefix if present
	if strings.HasPrefix(path, "/admin") {
		path = strings.TrimPrefix(path, "/admin")
		if path == "" {
			path = "/"
		}
	}

	// Try to serve the file directly
	if path != "/" && path != "" {
		// Check if file exists
		file, err := h.staticFS.Open(path)
		if err == nil {
			file.Close()
			// Serve static file
			c.Request.URL.Path = path
			h.fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
	}

	// For SPA routes (non-file paths), serve index.html
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Writer.Write(h.indexHTML)
}

// RegisterRoutes registers the admin UI routes on the given router group
func (h *AdminUIHandler) RegisterRoutes(router *gin.Engine) {
	// Serve admin UI at /admin/*
	router.GET("/admin", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/admin/")
	})
	router.GET("/admin/*filepath", func(c *gin.Context) {
		h.ServeHTTP(c)
	})
}
