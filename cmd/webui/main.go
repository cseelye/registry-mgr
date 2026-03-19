package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cseelye/registry-mgr/internal/config"
	"github.com/cseelye/registry-mgr/internal/models"
	"github.com/cseelye/registry-mgr/internal/registry"
	"github.com/spf13/cobra"
)

//go:embed templates
var templateFS embed.FS

type server struct {
	cfg    *config.Config
	client *registry.Client
	tmpl   *template.Template
}

type indexData struct {
	RegistryURL string
	Images      []*models.Image
	Error       string
}

var (
	cfg         *config.Config
	configFile  string
	registryURL string
	username    string
	password    string
	credFile    string
	port        int
	listenAddr  string
)

func main() {
	root := &cobra.Command{
		Use:   "registry-webui",
		Short: "Web UI for managing a private Docker registry",
		RunE:  run,
	}

	root.Flags().StringVar(&configFile, "config", "", "path to config file")
	root.Flags().StringVar(&registryURL, "registry", "", "registry URL (e.g. http://registry:5000)")
	root.Flags().StringVarP(&username, "username", "u", "", "registry username")
	root.Flags().StringVarP(&password, "password", "p", "", "registry password")
	root.Flags().StringVar(&credFile, "credentials-file", "", "path to credentials file (username:password)")
	root.Flags().IntVar(&port, "port", 0, "port to listen on (default 5080)")
	root.Flags().StringVar(&listenAddr, "listen", "", "address to listen on (default 0.0.0.0)")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	var err error
	cfg, err = config.Load(configFile)
	if err != nil {
		return err
	}
	// Apply flag overrides
	if registryURL != "" {
		cfg.RegistryURL = registryURL
	}
	if username != "" {
		cfg.Username = username
	}
	if password != "" {
		cfg.Password = password
	}
	if credFile != "" {
		if err := config.ApplyCredentialsFile(cfg, credFile); err != nil {
			return err
		}
	}
	if port > 0 {
		cfg.Port = port
	}
	if listenAddr != "" {
		cfg.ListenAddr = listenAddr
	}
	if cfg.RegistryURL == "" {
		return fmt.Errorf("registry URL is required (--registry, REGISTRY_MGR_URL, or config file)")
	}

	funcMap := template.FuncMap{
		"formatBytes": formatBytes,
		"formatTime":  formatTime,
	}
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return fmt.Errorf("parsing templates: %w", err)
	}

	client := registry.NewClient(cfg.RegistryURL, cfg.Username, cfg.Password)
	s := &server{cfg: cfg, client: client, tmpl: tmpl}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("POST /delete", s.handleDelete)

	addr := fmt.Sprintf("%s:%d", cfg.ListenAddr, cfg.Port)
	fmt.Printf("Registry Manager listening on http://%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := indexData{RegistryURL: s.cfg.RegistryURL}

	repos, err := s.client.ListRepositories(ctx)
	if err != nil {
		data.Error = friendlyError(err)
		s.render(w, data)
		return
	}

	for _, repo := range repos {
		tags, err := s.client.ListTags(ctx, repo)
		if err != nil {
			data.Error = friendlyError(err)
			s.render(w, data)
			return
		}
		for _, tag := range tags {
			img, err := s.client.GetImageDetails(ctx, repo, tag)
			if err != nil {
				data.Error = friendlyError(err)
				s.render(w, data)
				return
			}
			data.Images = append(data.Images, img)
		}
	}

	s.render(w, data)
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	images := r.Form["images"]
	var errs []string

	for _, item := range images {
		idx := strings.LastIndex(item, ":")
		if idx < 0 {
			errs = append(errs, fmt.Sprintf("invalid format %q", item))
			continue
		}
		repo, tag := item[:idx], item[idx+1:]
		if err := s.client.DeleteTag(ctx, repo, tag); err != nil {
			errs = append(errs, fmt.Sprintf("%s:%s: %v", repo, tag, friendlyError(err)))
		}
	}

	if len(errs) > 0 {
		// Re-render index with error message
		data := indexData{
			RegistryURL: s.cfg.RegistryURL,
			Error:       "Delete failed: " + strings.Join(errs, "; "),
		}
		s.render(w, data)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) render(w http.ResponseWriter, data indexData) {
	if err := s.tmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

func friendlyError(err error) string {
	if errors.Is(err, registry.ErrUnauthorized) {
		return "Authentication failed — check registry credentials (username/password)"
	}
	return err.Error()
}
